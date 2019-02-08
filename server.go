package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"google.golang.org/grpc"

	"github.com/gorilla/context"

	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrstakepool/controllers"
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
	log.Infof("Version: %s", version())
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

	// Setup static files
	assetHandler := http.StripPrefix("/assets/",
		http.FileServer(http.Dir(cfg.PublicPath)))

	// Apply middleware
	app := web.New()
	app.Handle("/assets/*", assetHandler)

	app.Use(middleware.RequestID)
	app.Use(middleware.Logger) // TODO: reimplement to use our logger
	app.Use(middleware.Recoverer)

	// Execute various middleware functions.  The order is very important
	// as each function establishes part of the application environment/context
	// that the next function will assume has been setup successfully.
	app.Use(application.ApplyTemplates)
	app.Use(application.ApplySessions)
	app.Use(application.ApplyCaptcha) // must be after ApplySessions
	app.Use(application.ApplyDbMap)
	app.Use(application.ApplyAPI)
	app.Use(application.ApplyAuth) // must be after ApplySessions
	app.Use(application.ApplyIsXhr)
	app.Use(application.ApplyCsrfProtection) // must be after ApplySessions
	app.Use(context.ClearHandler)

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

	controller, err := controllers.NewMainController(activeNetParams.Params,
		cfg.AdminIPs, cfg.AdminUserIDs, cfg.APISecret, APIVersionsSupported,
		cfg.BaseURL, cfg.ClosePool, cfg.ClosePoolMsg, cfg.EnableStakepoold,
		cfg.ColdWalletExtPub, grpcConnections, cfg.PoolFees, cfg.PoolEmail,
		cfg.PoolLink, cfg.SMTPFrom, cfg.SMTPHost, cfg.SMTPUsername,
		cfg.SMTPPassword, cfg.Version, cfg.WalletHosts, cfg.WalletCerts,
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
	controller.CheckAndResetUserVoteBits(application.DbMap)

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

	// Couple of files - in the real world you would use nginx to serve them.
	app.Get("/robots.txt", http.FileServer(http.Dir(cfg.PublicPath)))
	app.Get("/favicon.ico", http.FileServer(http.Dir(cfg.PublicPath+"/images")))

	// Home page
	app.Get("/", application.Route(controller, "Index"))

	// Admin tickets page
	app.Get("/admintickets", application.Route(controller, "AdminTickets"))
	app.Post("/admintickets", application.Route(controller, "AdminTicketsPost"))
	// Admin status page
	app.Get("/status", application.Route(controller, "AdminStatus"))

	// Address form
	app.Get("/address", application.Route(controller, "Address"))
	app.Post("/address", application.Route(controller, "AddressPost"))

	// API
	app.Handle("/api/v1/:command", application.APIHandler(controller.API))
	app.Handle("/api/v2/:command", application.APIHandler(controller.API))
	app.Handle("/api/*", gojify(system.APIInvalidHandler))

	// Email change/update confirmation
	app.Get("/emailupdate", application.Route(controller, "EmailUpdate"))

	// Email verification
	app.Get("/emailverify", application.Route(controller, "EmailVerify"))

	// Error page
	app.Get("/error", application.Route(controller, "Error"))

	// Password Reset routes
	app.Get("/passwordreset", application.Route(controller, "PasswordReset"))
	app.Post("/passwordreset", application.Route(controller, "PasswordResetPost"))

	// Password Update routes
	app.Get("/passwordupdate", application.Route(controller, "PasswordUpdate"))
	app.Post("/passwordupdate", application.Route(controller, "PasswordUpdatePost"))

	// Settings routes
	app.Get("/settings", application.Route(controller, "Settings"))
	app.Post("/settings", application.Route(controller, "SettingsPost"))

	// Sign In routes
	app.Get("/signin", application.Route(controller, "SignIn"))
	app.Post("/signin", application.Route(controller, "SignInPost"))

	// Sign Up routes
	app.Get("/signup", application.Route(controller, "SignUp"))
	app.Post("/signup", application.Route(controller, "SignUpPost"))

	// Captcha
	app.Get("/captchas/*", controller.CaptchaServe)
	app.Post("/verifyhuman", controller.CaptchaVerify)

	// Stats
	app.Get("/stats", application.Route(controller, "Stats"))

	// Tickets
	app.Get("/tickets", application.Route(controller, "Tickets"))

	// Voting routes
	app.Get("/voting", application.Route(controller, "Voting"))
	app.Post("/voting", application.Route(controller, "VotingPost"))

	// KTHXBYE
	app.Get("/logout", application.Route(controller, "Logout"))

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
