package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/golang/glog"
	"github.com/gorilla/context"

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
		cfg.ColdWalletExtPub, cfg.PoolFees)
	if err != nil {
		application.Close()
		dcrstakepoolLog.Errorf("Failed to initialize the main controller: %v",
			err)
		fmt.Fprintf(os.Stderr, "Fatal error in controller init: %v",
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

	// Error page
	goji.Get("/error", application.Route(controller, "Error"))

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
