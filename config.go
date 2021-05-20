// Copyright (c) 2013-2014 The btcsuite developers
// Copyright (c) 2015-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/decred/dcrstakepool/internal/version"
	flags "github.com/jessevdk/go-flags"
)

const (
	defaultBaseURL         = "http://127.0.0.1:8000"
	defaultClosePoolMsg    = "The voting service is temporarily closed to new signups."
	defaultConfigFilename  = "dcrstakepool.conf"
	defaultLogLevel        = "info"
	defaultLogDirname      = "logs"
	defaultLogFilename     = "dcrstakepool.log"
	defaultCookieSecure    = false
	defaultDBHost          = "localhost"
	defaultDBName          = "stakepool"
	defaultDBPort          = "3306"
	defaultDBUser          = "stakepool"
	defaultListen          = ":8000"
	defaultPoolEmail       = "admin@example.com"
	defaultPoolFees        = 7.5
	defaultPoolLink        = "https://forum.decred.org/threads/rfp-6-setup-and-operate-10-stake-pools.1361/"
	defaultPublicPath      = "public"
	defaultTemplatePath    = "views"
	defaultSMTPHost        = ""
	defaultMaxVotedTickets = 1000
	defaultDescription     = ""
	defaultDesignation     = ""
)

var (
	dcrstakepoolHomeDir = dcrutil.AppDataDir("dcrstakepool", false)
	defaultConfigFile   = filepath.Join(dcrstakepoolHomeDir, defaultConfigFilename)
	defaultLogDir       = filepath.Join(dcrstakepoolHomeDir, defaultLogDirname)
)

// runServiceCommand is only set to a real function on Windows.  It is used
// to parse and execute service commands specified via the -s flag.
var runServiceCommand func(string) error

// config defines the configuration options for dcrd.
//
// See loadConfig for details on the configuration load process.
type config struct {
	ShowVersion        bool    `short:"V" long:"version" description:"Display version information and exit"`
	ConfigFile         string  `short:"C" long:"configfile" description:"Path to configuration file"`
	LogDir             string  `long:"logdir" description:"Directory to log output."`
	Listen             string  `long:"listen" description:"Listen for connections on the specified interface/port (default all interfaces port: 9113, testnet: 19113)"`
	TestNet            bool    `long:"testnet" description:"Use the test network"`
	SimNet             bool    `long:"simnet" description:"Use the simulation test network"`
	DebugLevel         string  `short:"d" long:"debuglevel" description:"Logging level for all subsystems {trace, debug, info, warn, error, critical} -- You may also specify <subsystem>=<level>,<subsystem2>=<level>,... to set the log level for individual subsystems -- Use show to list available subsystems"`
	APISecret          string  `long:"apisecret" description:"Secret string used to encrypt API tokens."`
	BaseURL            string  `long:"baseurl" description:"BaseURL to use when sending links via email"`
	ColdWalletExtPub   string  `long:"coldwalletextpub" description:"The extended public key for addresses to which voting service user fees are sent."`
	ClosePool          bool    `long:"closepool" description:"Disable user registration actions (sign-ups and submitting addresses)"`
	ClosePoolMsg       string  `long:"closepoolmsg" description:"Message to display when closepool is set."`
	CookieSecret       string  `long:"cookiesecret" description:"Secret string used to encrypt session data."`
	CookieSecure       bool    `long:"cookiesecure" description:"Set whether cookies can be sent in clear text or not."`
	DBHost             string  `long:"dbhost" description:"Hostname for database connection"`
	DBUser             string  `long:"dbuser" description:"Username for database connection"`
	DBPassword         string  `long:"dbpassword" description:"Password for database connection"`
	DBPort             string  `long:"dbport" description:"Port for database connection"`
	DBName             string  `long:"dbname" description:"Name of database"`
	PublicPath         string  `long:"publicpath" description:"Path to the public folder which contains css/fonts/images/javascript."`
	TemplatePath       string  `long:"templatepath" description:"Path to the views folder which contains html files."`
	PoolEmail          string  `long:"poolemail" description:"Email address to for support inquiries"`
	PoolFees           float64 `long:"poolfees" description:"The per-ticket fees the user must send to the pool with their tickets"`
	PoolLink           string  `long:"poollink" description:"URL for support inquiries such as forum, IRC, etc"`
	RealIPHeader       string  `long:"realipheader" description:"The name of an HTTP request header containing the actual remote client IP address, typically set by a reverse proxy. An empty string (default) indicates to use net/Request.RemodeAddr."`
	SMTPFrom           string  `long:"smtpfrom" description:"From address to use on outbound mail"`
	SMTPHost           string  `long:"smtphost" description:"SMTP hostname/ip and port, e.g. mail.example.com:25"`
	SMTPUsername       string  `long:"smtpusername" description:"SMTP username for authentication if required"`
	SMTPPassword       string  `long:"smtppassword" description:"SMTP password for authentication if required"`
	UseSMTPS           bool    `long:"usesmtps" description:"Connect to the SMTP server using smtps."`
	SMTPSkipVerify     bool    `long:"smtpskipverify" description:"Skip SMTP TLS cert verification. Will only skip if SMTPCert is empty"`
	SMTPCert           string  `long:"smtpcert" description:"Path for the smtp certificate file"`
	SystemCerts        *x509.CertPool
	StakepooldHosts    []string `long:"stakepooldhosts" description:"Hostnames for stakepoold servers"`
	StakepooldCerts    []string `long:"stakepooldcerts" description:"Certificate paths for stakepoold servers"`
	VotingWalletExtPub string   `long:"votingwalletextpub" description:"The extended public key of the default account of the voting wallet"`
	AdminIPs           []string `long:"adminips" description:"Expected admin host"`
	AdminUserIDs       []string `long:"adminuserids" description:"User IDs of users who are allowed to access administrative functions."`
	MaxVotedTickets    int      `long:"maxvotedtickets" description:"Maximum number of voted tickets to show on tickets page."`
	Description        string   `long:"description" description:"Operators own description of their VSP"`
	Designation        string   `long:"designation" description:"VSP designation (eg. Alpha, Bravo, etc)"`

	coldWalletFeeKey    *hdkeychain.ExtendedKey
	votingWalletVoteKey *hdkeychain.ExtendedKey
}

// serviceOptions defines the configuration options for the daemon as a service
// on Windows.
type serviceOptions struct {
	ServiceCommand string `short:"s" long:"service" description:"Service command {install, remove, start, stop}"`
}

// cleanAndExpandPath expands environment variables and leading ~ in the
// passed path, cleans the result, and returns it.
func cleanAndExpandPath(path string) string {
	// Expand initial ~ to OS specific home directory.
	if strings.HasPrefix(path, "~") {
		homeDir := filepath.Dir(dcrstakepoolHomeDir)
		path = strings.Replace(path, "~", homeDir, 1)
	}

	// NOTE: The os.ExpandEnv doesn't work with Windows-style %VARIABLE%,
	// but they variables can still be expanded via POSIX-style $VARIABLE.
	return filepath.Clean(os.ExpandEnv(path))
}

// validLogLevel returns whether or not logLevel is a valid debug log level.
func validLogLevel(logLevel string) bool {
	switch logLevel {
	case "trace":
		fallthrough
	case "debug":
		fallthrough
	case "info":
		fallthrough
	case "warn":
		fallthrough
	case "error":
		fallthrough
	case "critical":
		return true
	}
	return false
}

// supportedSubsystems returns a sorted slice of the supported subsystems for
// logging purposes.
func supportedSubsystems() []string {
	// Convert the subsystemLoggers map keys to a slice.
	subsystems := make([]string, 0, len(subsystemLoggers))
	for subsysID := range subsystemLoggers {
		subsystems = append(subsystems, subsysID)
	}

	// Sort the subsystems for stable display.
	sort.Strings(subsystems)
	return subsystems
}

// parseAndSetDebugLevels attempts to parse the specified debug level and set
// the levels accordingly.  An appropriate error is returned if anything is
// invalid.
func parseAndSetDebugLevels(debugLevel string) error {
	// When the specified string doesn't have any delimiters, treat it as
	// the log level for all subsystems.
	if !strings.Contains(debugLevel, ",") && !strings.Contains(debugLevel, "=") {
		// Validate debug log level.
		if !validLogLevel(debugLevel) {
			str := "The specified debug level [%v] is invalid"
			return fmt.Errorf(str, debugLevel)
		}

		// Change the logging level for all subsystems.
		setLogLevels(debugLevel)

		return nil
	}

	// Split the specified string into subsystem/level pairs while detecting
	// issues and update the log levels accordingly.
	for _, logLevelPair := range strings.Split(debugLevel, ",") {
		if !strings.Contains(logLevelPair, "=") {
			str := "The specified debug level contains an invalid " +
				"subsystem/level pair [%v]"
			return fmt.Errorf(str, logLevelPair)
		}

		// Extract the specified subsystem and log level.
		fields := strings.Split(logLevelPair, "=")
		subsysID, logLevel := fields[0], fields[1]

		// Validate subsystem.
		if _, exists := subsystemLoggers[subsysID]; !exists {
			str := "The specified subsystem [%v] is invalid -- " +
				"supported subsytems %v"
			return fmt.Errorf(str, subsysID, supportedSubsystems())
		}

		// Validate log level.
		if !validLogLevel(logLevel) {
			str := "The specified debug level [%v] is invalid"
			return fmt.Errorf(str, logLevel)
		}

		setLogLevel(subsysID, logLevel)
	}

	return nil
}

// removeDuplicateAddresses returns a new slice with all duplicate entries in
// addrs removed.
func removeDuplicateAddresses(addrs []string) []string {
	result := make([]string, 0, len(addrs))
	seen := map[string]struct{}{}
	for _, val := range addrs {
		if _, ok := seen[val]; !ok {
			result = append(result, val)
			seen[val] = struct{}{}
		}
	}
	return result
}

// normalizeAddress returns addr with the passed default port appended if
// there is not already a port specified.
func normalizeAddress(addr, defaultPort string) string {
	_, _, err := net.SplitHostPort(addr)
	if err != nil {
		return net.JoinHostPort(addr, defaultPort)
	}
	return addr
}

// normalizeAddresses returns a new slice with all the passed peer addresses
// normalized with the given default port, and all duplicates removed.
func normalizeAddresses(addrs []string, defaultPort string) []string {
	for i, addr := range addrs {
		addrs[i] = normalizeAddress(addr, defaultPort)
	}

	return removeDuplicateAddresses(addrs)
}

// filesExists reports whether the named file or directory exists.
func fileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

// validate pub vote and fee keys as belonging to the network
func (c *config) parsePubKeys(params *chaincfg.Params) error {
	// Parse the extended public key and the pool fees.
	var err error
	c.coldWalletFeeKey, err = hdkeychain.NewKeyFromString(c.ColdWalletExtPub, params)
	if err != nil {
		return fmt.Errorf("cold wallet extended public key: %v", err)
	}
	// Parse the extended public key for the voting addresses.
	c.votingWalletVoteKey, err = hdkeychain.NewKeyFromString(c.VotingWalletExtPub, params)
	if err != nil {
		return fmt.Errorf("voting wallet extended public key: %v", err)
	}
	return nil
}

// newConfigParser returns a new command line flags parser.
func newConfigParser(cfg *config, so *serviceOptions, options flags.Options) *flags.Parser {
	parser := flags.NewParser(cfg, options)
	if runtime.GOOS == "windows" {
		parser.AddGroup("Service Options", "Service Options", so)
	}
	return parser
}

// loadConfig initializes and parses the config using a config file and command
// line options.
//
// The configuration proceeds as follows:
// 	1) Start with a default config with sane settings
// 	2) Pre-parse the command line to check for an alternative config file
// 	3) Load configuration file overwriting defaults with any specified options
// 	4) Parse CLI options and overwrite/add any specified options
//
// The above results in daemon functioning properly without any config settings
// while still allowing the user to override settings with config files and
// command line options.  Command line options always take precedence.
func loadConfig() (*config, []string, error) {
	// Default config.
	cfg := config{
		BaseURL:         defaultBaseURL,
		ClosePool:       false,
		ClosePoolMsg:    defaultClosePoolMsg,
		ConfigFile:      defaultConfigFile,
		DebugLevel:      defaultLogLevel,
		LogDir:          defaultLogDir,
		CookieSecure:    defaultCookieSecure,
		DBHost:          defaultDBHost,
		DBName:          defaultDBName,
		DBPort:          defaultDBPort,
		DBUser:          defaultDBUser,
		Listen:          defaultListen,
		PoolEmail:       defaultPoolEmail,
		PoolFees:        defaultPoolFees,
		PoolLink:        defaultPoolLink,
		PublicPath:      defaultPublicPath,
		TemplatePath:    defaultTemplatePath,
		SMTPHost:        defaultSMTPHost,
		MaxVotedTickets: defaultMaxVotedTickets,
		Description:     defaultDescription,
		Designation:     defaultDesignation,
	}

	// Service options which are only added on Windows.
	serviceOpts := serviceOptions{}

	// Pre-parse the command line options to see if an alternative config
	// file or the version flag was specified.  Any errors aside from the
	// help message error can be ignored here since they will be caught by
	// the final parse below.
	preCfg := cfg
	preParser := newConfigParser(&preCfg, &serviceOpts, flags.HelpFlag)
	_, err := preParser.Parse()
	if err != nil {
		var e *flags.Error
		if errors.As(err, &e) && e.Type == flags.ErrHelp {
			fmt.Fprintln(os.Stderr, err)
			return nil, nil, err
		}
		return nil, nil, err
	}

	// Show the version and exit if the version flag was specified.
	appName := filepath.Base(os.Args[0])
	appName = strings.TrimSuffix(appName, filepath.Ext(appName))
	usageMessage := fmt.Sprintf("Use %s -h to show usage", appName)
	if preCfg.ShowVersion {
		fmt.Printf("%s version %s (Go version %s %s/%s)\n", appName,
			version.String(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	// Perform service command and exit if specified.  Invalid service
	// commands show an appropriate error.  Only runs on Windows since
	// the runServiceCommand function will be nil when not on Windows.
	if serviceOpts.ServiceCommand != "" && runServiceCommand != nil {
		err := runServiceCommand(serviceOpts.ServiceCommand)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(0)
	}

	// Load additional config from file.
	parser := newConfigParser(&cfg, &serviceOpts, flags.Default)
	if !(preCfg.SimNet) || preCfg.ConfigFile != defaultConfigFile {
		err := flags.NewIniParser(parser).ParseFile(preCfg.ConfigFile)
		if err != nil {
			var e *os.PathError
			if !errors.As(err, &e) {
				fmt.Fprintf(os.Stderr, "Error parsing config "+
					"file: %v\n", err)
				fmt.Fprintln(os.Stderr, usageMessage)
				return nil, nil, err
			}
			return nil, nil, err
		}
	}

	// Parse command line options again to ensure they take precedence.
	remainingArgs, err := parser.Parse()
	if err != nil {
		var e *flags.Error
		if !errors.As(err, &e) || e.Type != flags.ErrHelp {
			fmt.Fprintln(os.Stderr, usageMessage)
		}
		return nil, nil, err
	}

	// Create the home directory if it doesn't already exist.
	funcName := "loadConfig"
	err = os.MkdirAll(dcrstakepoolHomeDir, 0700)
	if err != nil {
		// Show a nicer error message if it's because a symlink is
		// linked to a directory that does not exist (probably because
		// it's not mounted).
		var e *os.PathError
		if errors.As(err, &e) && os.IsExist(err) {
			if link, lerr := os.Readlink(e.Path); lerr == nil {
				str := "is symlink %s -> %s mounted?"
				err = fmt.Errorf(str, e.Path, link)
			}
		}

		str := "%s: Failed to create home directory: %v"
		err := fmt.Errorf(str, funcName, err)
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	// Multiple networks can't be selected simultaneously.
	var numNets int

	// Assign active network params and min required backend servers
	var minRequiredBackendServers = 2
	activeNetParams = &mainNetParams
	if cfg.TestNet {
		numNets++
		activeNetParams = &testNet3Params
		minRequiredBackendServers = 1
	}
	if cfg.SimNet {
		numNets++
		// Also disable dns seeding on the simulation test network.
		activeNetParams = &simNetParams
		minRequiredBackendServers = 1
	}
	if numNets > 1 {
		str := "%s: The testnet and simnet params can't be " +
			"used together -- choose one of the three"
		err := fmt.Errorf(str, funcName)
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Append the network type to the log directory so it is "namespaced"
	// per network in the same fashion as the data directory.
	cfg.LogDir = cleanAndExpandPath(cfg.LogDir)
	cfg.LogDir = filepath.Join(cfg.LogDir, netName(activeNetParams))

	// Special show command to list supported subsystems and exit.
	if cfg.DebugLevel == "show" {
		fmt.Println("Supported subsystems", supportedSubsystems())
		os.Exit(0)
	}

	// Initialize log rotation.  After log rotation has been initialized, the
	// logger variables may be used.
	initLogRotator(filepath.Join(cfg.LogDir, defaultLogFilename))

	// Parse, validate, and set debug log level(s).
	if err := parseAndSetDebugLevels(cfg.DebugLevel); err != nil {
		err := fmt.Errorf("%s: %v", funcName, err.Error())
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	if cfg.APISecret == "" {
		str := "%s: APIsecret is not set in config"
		err := fmt.Errorf(str, funcName)
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	if cfg.CookieSecret == "" {
		str := "%s: cookiesecret is not set in config"
		err := fmt.Errorf(str, funcName)
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	if cfg.DBPassword == "" {
		str := "%s: dbpassword is not set in config"
		err := fmt.Errorf(str, funcName)
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	if len(cfg.ColdWalletExtPub) == 0 {
		str := "%s: coldwalletextpub is not set in config"
		err := fmt.Errorf(str, funcName)
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	if len(cfg.AdminIPs) == 0 {
		str := "%s: adminips is not set in config"
		err := fmt.Errorf(str, funcName)
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	if len(cfg.AdminUserIDs) == 0 {
		str := "%s: adminuserids is not set in config"
		err := fmt.Errorf(str, funcName)
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	if len(cfg.VotingWalletExtPub) == 0 {
		str := "%s: votingwalletextpub is not set in config"
		err := fmt.Errorf(str, funcName)
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	if err := cfg.parsePubKeys(activeNetParams.Params); err != nil {
		err := fmt.Errorf("%s: failed to parse extended public keys: %v", funcName, err)
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	// Convert comma separated list into a slice
	cfg.AdminIPs = strings.Split(cfg.AdminIPs[0], ",")
	cfg.AdminUserIDs = strings.Split(cfg.AdminUserIDs[0], ",")

	if len(cfg.StakepooldHosts) == 0 {
		str := "%s: stakepooldhosts is not set in config"
		err := fmt.Errorf(str, funcName)
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	if len(cfg.StakepooldCerts) == 0 {
		str := "%s: stakepooldcerts is not set in config"
		err := fmt.Errorf(str, funcName)
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	cfg.StakepooldHosts = strings.Split(cfg.StakepooldHosts[0], ",")
	cfg.StakepooldCerts = strings.Split(cfg.StakepooldCerts[0], ",")

	// Add default stakepoold port for the active network if there's
	// no port specified
	cfg.StakepooldHosts = normalizeAddresses(cfg.StakepooldHosts,
		activeNetParams.StakepooldRPCServerPort)
	if len(cfg.StakepooldHosts) < minRequiredBackendServers {
		str := "%s: you must specify at least %d stakepooldhosts"
		err := fmt.Errorf(str, funcName, minRequiredBackendServers)
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	if len(cfg.StakepooldHosts) != len(cfg.StakepooldCerts) {
		str := "%s: wallet configuration mismatch " +
			"(stakepooldcerts and stakepooldhosts " +
			"counts differ)"
		err := fmt.Errorf(str, funcName)
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	for idx := range cfg.StakepooldCerts {
		if !fileExists(cfg.StakepooldCerts[idx]) {
			path := filepath.Join(dcrstakepoolHomeDir,
				cfg.StakepooldCerts[idx])
			if !fileExists(path) {
				str := "%s: stakepooldcert " +
					cfg.StakepooldCerts[idx] +
					" and " + path + " don't exist"
				err := fmt.Errorf(str, funcName)
				fmt.Fprintln(os.Stderr, err)
				return nil, nil, err
			}

			cfg.StakepooldCerts[idx] = path
		}
	}

	// Validate smtp root cert.
	if cfg.SMTPCert != "" {
		cfg.SMTPCert = cleanAndExpandPath(cfg.SMTPCert)

		b, err := ioutil.ReadFile(cfg.SMTPCert)
		if err != nil {
			return nil, nil, fmt.Errorf("read smtpcert: %v", err)
		}
		block, _ := pem.Decode(b)
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, nil, fmt.Errorf("parse smtpcert: %v", err)
		}
		systemCerts, err := x509.SystemCertPool()
		if err != nil {
			return nil, nil, fmt.Errorf("getting systemcertpool: %v", err)
		}
		systemCerts.AddCert(cert)
		cfg.SystemCerts = systemCerts

		if cfg.SMTPSkipVerify {
			log.Warnf("SMTPCert has been set so SMTPSkipVerify is being disregarded.")
		}
	}

	return &cfg, remainingArgs, nil
}
