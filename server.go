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

	"google.golang.org/grpc"

	"github.com/gorilla/context"
	"github.com/gorilla/csrf"

	"github.com/decred/dcrd/rpcclient/v2"
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

func runMain() int {
	// Load configuration and parse command line.  This function also
	// initializes logging and configures it accordingly.
	loadedCfg, _, err := loadConfig()
	if err != nil {
		return 1
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
		log.Critical("Failed to open database.")
		return 7
	}
	if err = application.LoadTemplates(cfg.TemplatePath); err != nil {
		log.Criticalf("Failed to load templates: %v", err)
		return 2
	}

	// Set up signal handler
	// SIGUSR1 = Reload html templates (On nix systems)
	system.ReloadTemplatesSig(application)

	rpcclient.UseLogger(log)

	// Supported API versions are advertised in the API stats result
	APIVersionsSupported := []int{1, 2}

	grpcConnections := make([]*grpc.ClientConn, len(cfg.StakepooldHosts))

	if cfg.EnableStakepoold {
		for i := range cfg.StakepooldHosts {
			grpcConnections[i], err = stakepooldclient.ConnectStakepooldGRPC(cfg.StakepooldHosts, cfg.StakepooldCerts, i)
			if err != nil {
				log.Errorf("Failed to connect to stakepoold host %d: %v", i, err)
				return 8
			}
		}
	}

	sender, err := email.NewSender(cfg.SMTPHost, cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPFrom, cfg.UseSMTPS)
	if err != nil {
		application.Close()
		log.Errorf("Failed to initialize the smtp server: %v", err)
		return 1
	}

	controller, err := controllers.NewMainController(activeNetParams.Params,
		cfg.AdminIPs, cfg.AdminUserIDs, cfg.APISecret, APIVersionsSupported,
		cfg.BaseURL, cfg.ClosePool, cfg.ClosePoolMsg, cfg.EnableStakepoold,
		cfg.ColdWalletExtPub, grpcConnections, cfg.PoolFees, cfg.PoolEmail,
		cfg.PoolLink, sender, cfg.WalletHosts, cfg.WalletCerts,
		cfg.WalletUsers, cfg.WalletPasswords, cfg.MinServers, cfg.RealIPHeader,
		cfg.VotingWalletExtPub, cfg.MaxVotedAge)
	if err != nil {
		application.Close()
		log.Errorf("Failed to initialize the main controller: %v",
			err)
		fmt.Fprintf(os.Stderr, "Fatal error in controller init: %v",
			err)
		return 3
	}

	// reset votebits if Vote Version changed or stored VoteBits are invalid
	_, err = controller.CheckAndResetUserVoteBits(application.DbMap)
	if err != nil {
		application.Close()
		log.Errorf("failed to check and reset user vote bits: %v",
			err)
		fmt.Fprintf(os.Stderr, "failed to check and reset user vote bits: %v",
			err)
		return 3
	}

	if cfg.EnableStakepoold {
		err = controller.StakepooldUpdateAll(application.DbMap, controllers.StakepooldUpdateKindAll)
		if err != nil {
			log.Errorf("TriggerStakepooldUpdates failed: %v", err)
			return 9
		}
		for i := range grpcConnections {
			addedLowFeeTickets, err := stakepooldclient.StakepooldGetAddedLowFeeTickets(grpcConnections[i])
			if err != nil {
				log.Errorf("GetAddedLowFeeTickets failed on host %d: %v", i, err)
				return 9
			}
			ignoredLowFeeTickets, err := stakepooldclient.StakepooldGetIgnoredLowFeeTickets(grpcConnections[i])
			if err != nil {
				log.Errorf("GetIgnoredLowFeeTickets failed on host %d: %v", i, err)
				return 9
			}
			liveTickets, err := stakepooldclient.StakepooldGetLiveTickets(grpcConnections[i])
			if err != nil {
				log.Errorf("GetLiveTickets failed on host %d: %v", i, err)
				return 9
			}
			log.Infof("stakepoold %d reports ticket totals of AddedLowFee %v "+
				"IgnoredLowFee %v Live %v", i, len(addedLowFeeTickets),
				len(ignoredLowFeeTickets), len(liveTickets))
		}
	}

	err = controller.RPCSync(application.DbMap)
	if err != nil {
		application.Close()
		log.Errorf("Failed to sync the wallets: %v",
			err)
		return 4
	}

	controller.RPCStart()

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

	// Execute various middleware functions.  The order is very important
	// as each function establishes part of the application environment/context
	// that the next function will assume has been setup successfully.
	html.Use(application.ApplyTemplates)
	html.Use(application.ApplySessions)
	html.Use(application.ApplyCaptcha) // must be after ApplySessions
	html.Use(application.ApplyAuth)    // must be after ApplySessions
	html.Use(csrf.Protect([]byte(cfg.APISecret), csrf.Secure(cfg.CookieSecure)))

	// Setup static files
	html.Get("/assets/*", http.StripPrefix("/assets/",
		http.FileServer(http.Dir(cfg.PublicPath))))
	html.Get("/robots.txt", http.FileServer(http.Dir(cfg.PublicPath)))
	html.Get("/favicon.ico", http.FileServer(http.Dir(cfg.PublicPath+"/images")))

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

	// Sign In routes
	html.Get("/signin", application.Route(controller, "SignIn"))
	html.Post("/signin", application.Route(controller, "SignInPost"))

	// Sign Up routes
	html.Get("/signup", application.Route(controller, "SignUp"))
	html.Post("/signup", application.Route(controller, "SignUpPost"))

	// Captcha
	html.Get("/captchas/*", controller.CaptchaServe)
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

	graceful.PostHook(func() {
		controller.RPCStop()
		application.Close()
	})
	app.Abandon(middleware.Logger)
	app.Compile()

	server := &http.Server{Handler: app}
	listener, err := listenTo(cfg.Listen)
	if err != nil {
		log.Errorf("could not bind %v", err)
		return 5
	}

	log.Infof("listening on %v", listener.Addr())

	if err = server.Serve(listener); err != nil {
		log.Errorf("Serve error: %s", err.Error())
		return 6
	}

	return 0
}

func main() {
	os.Exit(runMain())
}
