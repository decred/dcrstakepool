package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/golang/glog"
	"github.com/gorilla/context"

	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrstakepool/controllers"
	"github.com/decred/dcrstakepool/system"

	"github.com/zenazn/goji"
	"github.com/zenazn/goji/graceful"
	"github.com/zenazn/goji/web"
	"github.com/zenazn/goji/web/middleware"
)

var (
	cfg *config
)

func main() {
	// Load configuration and parse command line.  This function also
	// initializes logging and configures it accordingly.
	loadedCfg, _, err := loadConfig()
	if err != nil {
		os.Exit(1)
	}
	cfg = loadedCfg
	dcrstakepoolLog.Infof("Version: %s", version())
	dcrstakepoolLog.Infof("Network: %s", activeNetParams.Params.Name)

	filename := flag.String("config", "config.toml", "Path to configuration file")

	flag.Parse()
	defer glog.Flush()

	var application = &system.Application{}

	application.Init(filename)
	application.LoadTemplates()

	// Set up signal handler
	// SIGUSR1 = Reload html templates
	sigs := make(chan os.Signal, 1)

	signal.Notify(sigs, syscall.SIGUSR1)

	go func() {
		for {
			sig := <-sigs
			dcrstakepoolLog.Infof("Received: %s", sig)
			fmt.Fprintf(os.Stdout, "Received: %s\n", sig)
			if sig == syscall.SIGUSR1 {
				application.LoadTemplates()
				dcrstakepoolLog.Infof("LoadTemplates() executed.")
				fmt.Fprintf(os.Stdout, "LoadTemplates() executed.\n")
			}
		}
	}()

	dcrrpcclient.UseLogger(dcrstakepoolLog)

	// Setup static files
	static := web.New()
	publicPath := application.Config.Get("general.public_path").(string)
	static.Get("/assets/*", http.StripPrefix("/assets/",
		http.FileServer(http.Dir(publicPath))))

	http.Handle("/assets/", static)

	// Apply middleware
	goji.Use(application.ApplyTemplates)
	goji.Use(application.ApplySessions)
	goji.Use(application.ApplyDbMap)
	goji.Use(application.ApplyAuth)
	goji.Use(application.ApplyIsXhr)
	goji.Use(application.ApplyCsrfProtection)
	goji.Use(context.ClearHandler)

	controller, err := controllers.NewMainController(activeNetParams.Params,
		cfg.AdminIPs, cfg.BaseURL, cfg.ClosePool, cfg.ClosePoolMsg,
		cfg.ColdWalletExtPub, cfg.PoolEmail, cfg.PoolFees, cfg.PoolLink,
		cfg.RecaptchaSecret, cfg.RecaptchaSitekey, cfg.SMTPFrom, cfg.SMTPHost,
		cfg.SMTPUsername, cfg.SMTPPassword, cfg.Version,
		cfg.WalletHosts, cfg.WalletCerts, cfg.WalletUsers, cfg.WalletPasswords,
		cfg.MinServers)
	if err != nil {
		application.Close()
		dcrstakepoolLog.Errorf("Failed to initialize the main controller: %v",
			err)
		fmt.Fprintf(os.Stderr, "Fatal error in controller init: %v",
			err)
		os.Exit(1)
	}

	err = controller.RPCSync(application.DbMap, cfg.SkipVoteBitsSync)
	if err != nil {
		application.Close()
		dcrstakepoolLog.Errorf("Failed to sync the wallets: %v",
			err)
		fmt.Fprintf(os.Stderr, "Fatal error in rpc sync: %v",
			err)
		os.Exit(1)
	}

	controller.RPCStart()

	// Couple of files - in the real world you would use nginx to serve them.
	goji.Get("/robots.txt", http.FileServer(http.Dir(publicPath)))
	goji.Get("/favicon.ico", http.FileServer(http.Dir(publicPath+"/images")))

	// Home page
	goji.Get("/", application.Route(controller, "Index"))

	// Address form
	goji.Get("/address", application.Route(controller, "Address"))
	goji.Post("/address", application.Route(controller, "AddressPost"))

	// API
	goji.Handle("/api/*", application.Route(controller, "API"))

	// Email change/update confirmation
	goji.Get("/emailupdate", application.Route(controller, "EmailUpdate"))

	// Email verification
	goji.Get("/emailverify", application.Route(controller, "EmailVerify"))

	// Error page
	goji.Get("/error", application.Route(controller, "Error"))

	// Password Reset routes
	goji.Get("/passwordreset", application.Route(controller, "PasswordReset"))
	goji.Post("/passwordreset", application.Route(controller, "PasswordResetPost"))

	// Password Update routes
	goji.Get("/passwordupdate", application.Route(controller, "PasswordUpdate"))
	goji.Post("/passwordupdate", application.Route(controller, "PasswordUpdatePost"))

	// Settings routes
	goji.Get("/settings", application.Route(controller, "Settings"))
	goji.Post("/settings", application.Route(controller, "SettingsPost"))

	// Sign In routes
	goji.Get("/signin", application.Route(controller, "SignIn"))
	goji.Post("/signin", application.Route(controller, "SignInPost"))

	// Sign Up routes
	goji.Get("/signup", application.Route(controller, "SignUp"))
	goji.Post("/signup", application.Route(controller, "SignUpPost"))

	// Stats
	goji.Get("/stats", application.Route(controller, "Stats"))

	// Status
	goji.Get("/status", application.Route(controller, "Status"))

	// Tickets routes
	goji.Get("/tickets", application.Route(controller, "Tickets"))
	goji.Post("/tickets", application.Route(controller, "TicketsPost"))

	// KTHXBYE
	goji.Get("/logout", application.Route(controller, "Logout"))

	graceful.PostHook(func() {
		controller.RPCStop()
		application.Close()
	})
	goji.Abandon(middleware.Logger)
	goji.Serve()
}
