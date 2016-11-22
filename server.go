package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gorilla/context"

	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrstakepool/controllers"
	"github.com/decred/dcrstakepool/system"

	"github.com/zenazn/goji/graceful"
	"github.com/zenazn/goji/web"
	"github.com/zenazn/goji/web/middleware"
)

var (
	cfg *config
)

func listenTo(bind string) (net.Listener, error) {
	if strings.Contains(bind, ":") {
		return net.Listen("tcp", bind)
	} else if strings.HasPrefix(bind, ".") || strings.HasPrefix(bind, "/") {
		return net.Listen("unix", bind)
	}

	return nil, fmt.Errorf("error while parsing bind arg %v", bind)
}

func main() {
	// Load configuration and parse command line.  This function also
	// initializes logging and configures it accordingly.
	loadedCfg, _, err := loadConfig()
	if err != nil {
		os.Exit(1)
	}
	cfg = loadedCfg
	log.Infof("Version: %s", version())
	log.Infof("Network: %s", activeNetParams.Params.Name)

	var application = &system.Application{}

	application.Init(cfg.CookieSecret, cfg.CookieSecure,
		cfg.DBHost, cfg.DBName, cfg.DBPassword, cfg.DBPort, cfg.DBUser)
	application.LoadTemplates(cfg.TemplatePath)

	// Set up signal handler
	// SIGUSR1 = Reload html templates
	sigs := make(chan os.Signal, 1)

	signal.Notify(sigs, syscall.SIGUSR1)

	go func() {
		for {
			sig := <-sigs
			log.Infof("Received: %s", sig)
			if sig == syscall.SIGUSR1 {
				application.LoadTemplates(cfg.TemplatePath)
				log.Infof("LoadTemplates() executed.")
			}
		}
	}()

	dcrrpcclient.UseLogger(log)

	// Setup static files
	assetHandler := http.StripPrefix("/assets/",
		http.FileServer(http.Dir(cfg.PublicPath)))

	// Apply middleware
	app := web.New()
	app.Handle("/assets/*", assetHandler)

	app.Use(middleware.RequestID)
	app.Use(middleware.Logger) // TODO: reimplement to use our logger
	app.Use(middleware.Recoverer)

	app.Use(application.ApplyTemplates)
	app.Use(application.ApplySessions)
	app.Use(application.ApplyDbMap)
	app.Use(application.ApplyAuth)
	app.Use(application.ApplyIsXhr)
	app.Use(application.ApplyCsrfProtection)
	app.Use(context.ClearHandler)

	controller, err := controllers.NewMainController(activeNetParams.Params,
		cfg.AdminIPs, cfg.BaseURL, cfg.ClosePool, cfg.ClosePoolMsg,
		cfg.ColdWalletExtPub, cfg.PoolEmail, cfg.PoolFees, cfg.PoolLink,
		cfg.RecaptchaSecret, cfg.RecaptchaSitekey, cfg.SMTPFrom, cfg.SMTPHost,
		cfg.SMTPUsername, cfg.SMTPPassword, cfg.Version,
		cfg.WalletHosts, cfg.WalletCerts, cfg.WalletUsers, cfg.WalletPasswords,
		cfg.MinServers)
	if err != nil {
		application.Close()
		log.Errorf("Failed to initialize the main controller: %v",
			err)
		fmt.Fprintf(os.Stderr, "Fatal error in controller init: %v",
			err)
		os.Exit(1)
	}

	err = controller.RPCSync(application.DbMap, cfg.SkipVoteBitsSync)
	if err != nil {
		application.Close()
		log.Errorf("Failed to sync the wallets: %v",
			err)
		os.Exit(1)
	}

	controller.RPCStart()

	// Couple of files - in the real world you would use nginx to serve them.
	app.Get("/robots.txt", http.FileServer(http.Dir(cfg.PublicPath)))
	app.Get("/favicon.ico", http.FileServer(http.Dir(cfg.PublicPath+"/images")))

	// Home page
	app.Get("/", application.Route(controller, "Index"))

	// Address form
	app.Get("/address", application.Route(controller, "Address"))
	app.Post("/address", application.Route(controller, "AddressPost"))

	// API
	app.Handle("/api/*", application.Route(controller, "API"))

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

	// Stats
	app.Get("/stats", application.Route(controller, "Stats"))

	// Status
	app.Get("/status", application.Route(controller, "Status"))

	// Tickets routes
	app.Get("/tickets", application.Route(controller, "Tickets"))
	app.Post("/tickets", application.Route(controller, "TicketsPost"))

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
		os.Exit(1)
	}

	log.Infof("listening on %v", listener.Addr())

	server.Serve(listener)
}
