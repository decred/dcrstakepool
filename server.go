package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/golang/glog"
	"github.com/gorilla/context"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrstakepool/controllers"
	"github.com/decred/dcrstakepool/system"

	"github.com/zenazn/goji"
	"github.com/zenazn/goji/graceful"
	"github.com/zenazn/goji/web"
	"github.com/zenazn/goji/web/middleware"
)

// chainParams is the Decred network the pool uses.
var chainParams = &chaincfg.TestNetParams

var (
	cfg *config
)

func main() {
	// Load configuration and parse command line.  This function also
	// initializes logging and configures it accordingly.
	tcfg, _, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v", err)
		os.Exit(1)
	}
	cfg = tcfg
	dcrstakepoolLog.Infof("Version %s", version())

	filename := flag.String("config", "config.toml", "Path to configuration file")

	flag.Parse()
	defer glog.Flush()

	var application = &system.Application{}

	application.Init(filename)
	application.LoadTemplates()

	// Setup static files
	static := web.New()
	publicPath := application.Config.Get("general.public_path").(string)
	static.Get("/assets/*", http.StripPrefix("/assets/", http.FileServer(http.Dir(publicPath))))

	http.Handle("/assets/", static)

	// Apply middleware
	goji.Use(application.ApplyTemplates)
	goji.Use(application.ApplySessions)
	goji.Use(application.ApplyDbMap)
	goji.Use(application.ApplyAuth)
	goji.Use(application.ApplyIsXhr)
	goji.Use(application.ApplyCsrfProtection)
	goji.Use(context.ClearHandler)

	controller, err := controllers.NewMainController(chainParams)
	if err != nil {
		application.Close()
		dcrstakepoolLog.Errorf("Failed to initialize the main controller: %v", err)
		fmt.Fprintf(os.Stderr, "Failed to initialize the main controller: %v", err)
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

	// Stresstest
	if chainParams.Name == "testnet" {
		goji.Get("/stresstest", application.Route(controller, "Stresstest"))
	}

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
	flag.Set("bind", ":8000")
	goji.Serve()
}
