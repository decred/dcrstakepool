// Copyright (c) 2016-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/context"
	"github.com/gorilla/csrf"

	"github.com/decred/dcrd/rpcclient/v3"
	"github.com/decred/dcrstakepool/controllers"
	"github.com/decred/dcrstakepool/email"
	"github.com/decred/dcrstakepool/stakepooldclient"
	"github.com/decred/dcrstakepool/system"

	"github.com/zenazn/goji/graceful"
	"github.com/zenazn/goji/web"
	"github.com/zenazn/goji/web/middleware"
)

var (
	cfg *config
)

// gojify wraps system's GojiWebHandlerFunc to allow the use of an
// http.HanderFunc as a web.HandlerFunc.
func gojify(h http.HandlerFunc) web.HandlerFunc {
	return system.GojiWebHandlerFunc(h)
}

func listenTo(bind string) (net.Listener, error) {
	if strings.Contains(bind, ":") {
		return net.Listen("tcp", bind)
	} else if strings.HasPrefix(bind, ".") || strings.HasPrefix(bind, "/") {
		return net.Listen("unix", bind)
	}

	return nil, fmt.Errorf("error while parsing bind arg %v", bind)
}

func runMain() error {
	// Load configuration and parse command line.  This function also
	// initializes logging and configures it accordingly.
	loadedCfg, _, err := loadConfig()
	if err != nil {
		return fmt.Errorf("Failed to load config: %v", err)
	}
	cfg = loadedCfg
	log.Infof("Network: %s", activeNetParams.Params.Name)

	defer func() {
		if logRotator != nil {
			logRotator.Close()
		}
	}()

	var application = &system.Application{}

	application.Init(cfg.APISecret, cfg.BaseURL, cfg.CookieSecret,
		cfg.CookieSecure, cfg.DBHost, cfg.DBName, cfg.DBPassword, cfg.DBPort,
		cfg.DBUser)
	if application.DbMap == nil {
		return fmt.Errorf("Failed to open database.")
	}
	if err = application.LoadTemplates(cfg.TemplatePath); err != nil {
		return fmt.Errorf("Failed to load templates: %v", err)
	}

	// Set up signal handler
	// SIGUSR1 = Reload html templates (On nix systems)
	system.ReloadTemplatesSig(application)

	rpcclient.UseLogger(log)

	// Supported API versions are advertised in the API stats result
	APIVersionsSupported := []int{1, 2}

	var stakepooldConnMan *stakepooldclient.StakepooldManager

	stakepooldConnMan, err = stakepooldclient.ConnectStakepooldGRPC(cfg.StakepooldHosts, cfg.StakepooldCerts)
	if err != nil {
		return fmt.Errorf("Failed to connect to stakepoold host: %v", err)
	}

	var sender email.Sender
	if cfg.SMTPHost != "" {
		sender, err = email.NewSender(cfg.SMTPHost, cfg.SMTPUsername, cfg.SMTPPassword,
			cfg.SMTPFrom, cfg.UseSMTPS, cfg.SystemCerts, cfg.SMTPSkipVerify)
		if err != nil {
			application.Close()
			return fmt.Errorf("Failed to initialize the smtp server: %v", err)
		}
	}

	controllerCfg := controllers.Config{
		AdminIPs:        cfg.AdminIPs,
		AdminUserIDs:    cfg.AdminUserIDs,
		APISecret:       cfg.APISecret,
		BaseURL:         cfg.BaseURL,
		ClosePool:       cfg.ClosePool,
		ClosePoolMsg:    cfg.ClosePoolMsg,
		PoolEmail:       cfg.PoolEmail,
		PoolFees:        cfg.PoolFees,
		PoolLink:        cfg.PoolLink,
		RealIPHeader:    cfg.RealIPHeader,
		MaxVotedTickets: cfg.MaxVotedTickets,
		Description:     cfg.Description,
		Designation:     cfg.Designation,

		APIVersionsSupported: APIVersionsSupported,
		FeeXpub:              coldWalletFeeKey,
		StakepooldServers:    stakepooldConnMan,
		EmailSender:          sender,
		VotingXpub:           votingWalletVoteKey,
		NetParams:            activeNetParams.Params,
	}

	controller, err := controllers.NewMainController(&controllerCfg)

	if err != nil {
		application.Close()
		return fmt.Errorf("Failed to initialize the main controller: %v", err)
	}

	// Check that dcrstakepool config and all stakepoold configs
	// have the same value set for `coldwalletextpub`.
	if err = controller.Cfg.StakepooldServers.CrossCheckColdWalletExtPubs(cfg.ColdWalletExtPub); err != nil {
		application.Close()
		return err
	}

	// reset votebits if Vote Version changed or stored VoteBits are invalid
	_, err = controller.CheckAndResetUserVoteBits(application.DbMap)
	if err != nil {
		application.Close()
		return fmt.Errorf("failed to check and reset user vote bits: %v", err)
	}

	err = controller.StakepooldUpdateUsers(application.DbMap)
	if err != nil {
		return fmt.Errorf("StakepooldUpdateUsers failed: %v", err)
	}
	err = controller.StakepooldUpdateTickets(application.DbMap)
	if err != nil {
		return fmt.Errorf("StakepooldUpdateTickets failed: %v", err)
	}
	// Log the reported count of ignored/added/live tickets from each stakepoold
	_, err = controller.Cfg.StakepooldServers.GetIgnoredLowFeeTickets()
	if err != nil {
		return fmt.Errorf("StakepooldGetIgnoredLowFeeTickets failed: %v", err)
	}
	_, err = controller.Cfg.StakepooldServers.GetAddedLowFeeTickets()
	if err != nil {
		return fmt.Errorf("StakepooldGetAddedLowFeeTickets failed: %v", err)
	}
	_, err = controller.Cfg.StakepooldServers.GetLiveTickets()
	if err != nil {
		return fmt.Errorf("StakepooldGetLiveTickets failed: %v", err)
	}

	err = controller.RPCSync(application.DbMap)
	if err != nil {
		application.Close()
		return fmt.Errorf("Failed to sync the wallets: %v",
			err)
	}

	// Set up web server routes
	app := web.New()

	// Middlewares used by app are applied to all routes (HTML and API)
	app.Use(middleware.RequestID)
	app.Use(middleware.Logger) // TODO: reimplement to use our logger
	app.Use(middleware.Recoverer)

	app.Use(application.ApplyDbMap)

	app.Use(context.ClearHandler)

	// API routes
	api := web.New()

	api.Use(application.ApplyAPI)

	api.Handle("/api/v1/:command", application.APIHandler(controller.API))
	api.Handle("/api/v2/:command", application.APIHandler(controller.API))
	api.Handle("/api/*", gojify(system.APIInvalidHandler))

	// HTML routes
	html := web.New()
	// static routes
	static := web.New()

	// Execute various middleware functions.  The order is very important
	// as each function establishes part of the application environment/context
	// that the next function will assume has been setup successfully.
	html.Use(application.ApplyTemplates)
	html.Use(application.ApplySessions)
	html.Use(application.ApplyCaptcha) // must be after ApplySessions
	html.Use(application.ApplyAuth)    // must be after ApplySessions
	html.Use(csrf.Protect([]byte(cfg.APISecret), csrf.Secure(cfg.CookieSecure)))

	// Setup static files
	static.Get("/assets/*", http.StripPrefix("/assets/",
		http.FileServer(http.Dir(cfg.PublicPath))))

	// Home page
	html.Get("/", application.Route(controller, "Index"))

	// Admin tickets page
	html.Get("/admintickets", application.Route(controller, "AdminTickets"))
	html.Post("/admintickets", application.Route(controller, "AdminTicketsPost"))
	// Admin status page
	html.Get("/status", application.Route(controller, "AdminStatus"))

	// Address form
	html.Get("/address", application.Route(controller, "Address"))
	html.Post("/address", application.Route(controller, "AddressPost"))

	// Email change/update confirmation
	html.Get("/emailupdate", application.Route(controller, "EmailUpdate"))

	// Email verification
	html.Get("/emailverify", application.Route(controller, "EmailVerify"))

	// Error page
	html.Get("/error", application.Route(controller, "Error"))

	// Password Reset routes
	html.Get("/passwordreset", application.Route(controller, "PasswordReset"))
	html.Post("/passwordreset", application.Route(controller, "PasswordResetPost"))

	// Password Update routes
	html.Get("/passwordupdate", application.Route(controller, "PasswordUpdate"))
	html.Post("/passwordupdate", application.Route(controller, "PasswordUpdatePost"))

	// Settings routes
	html.Get("/settings", application.Route(controller, "Settings"))
	html.Post("/settings", application.Route(controller, "SettingsPost"))

	// Login routes
	html.Get("/login", application.Route(controller, "Login"))
	html.Post("/login", application.Route(controller, "LoginPost"))

	// Register routes
	html.Get("/register", application.Route(controller, "Register"))
	html.Post("/register", application.Route(controller, "RegisterPost"))

	// Captcha
	static.Get("/captchas/*", controller.CaptchaServe)
	html.Post("/verifyhuman", controller.CaptchaVerify)

	// Stats
	html.Get("/stats", application.Route(controller, "Stats"))

	// Tickets
	html.Get("/tickets", application.Route(controller, "Tickets"))

	// Voting routes
	html.Get("/voting", application.Route(controller, "Voting"))
	html.Post("/voting", application.Route(controller, "VotingPost"))

	// KTHXBYE
	html.Get("/logout", application.Route(controller, "Logout"))

	app.Handle("/api/*", api)
	app.Handle("/*", html)

	parent := web.New()
	parent.Handle("/assets/*", static)
	parent.Handle("/captchas/*", static)
	parent.Handle("/*", app)

	graceful.PostHook(func() {
		application.Close()
	})
	app.Abandon(middleware.Logger)
	app.Compile()

	server := &http.Server{Handler: parent}

	listener, err := listenTo(cfg.Listen)
	if err != nil {
		return fmt.Errorf("could not bind %v", err)
	}

	log.Infof("listening on %v", listener.Addr())

	if err = server.Serve(listener); err != nil {
		return fmt.Errorf("Serve error: %s", err.Error())
	}

	return nil
}

func main() {
	if err := runMain(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
