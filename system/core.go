package system

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/decred/dcrstakepool/models"
	"github.com/go-gorp/gorp"
	"github.com/gorilla/sessions"
	"github.com/zenazn/goji/web"

	"google.golang.org/grpc/codes"
)

// CSRF token constants
const (
	CSRFCookie = "XSRF-TOKEN"
	CSRFHeader = "X-XSRF-TOKEN"
	CSRFKey    = "csrf_token"
)

type Application struct {
	APISecret      string
	Template       *template.Template
	TemplatesPath  string
	Store          *sessions.CookieStore
	DbMap          *gorp.DbMap
	CsrfProtection *CsrfProtection
}

type CsrfProtection struct {
	Key    string
	Cookie string
	Header string
	Secure bool
}

// GojiWebHandlerFunc is an adaptor that allows an http.HanderFunc where a
// web.HandlerFunc is required.
func GojiWebHandlerFunc(h http.HandlerFunc) web.HandlerFunc {
	return func(_ web.C, w http.ResponseWriter, r *http.Request) {
		h(w, r)
	}
}

func (application *Application) Init(APISecret string, baseURL string,
	cookieSecret string, cookieSecure bool, DBHost string, DBName string,
	DBPassword string,
	DBPort string, DBUser string) {

	hash := sha256.New()
	io.WriteString(hash, cookieSecret)
	application.Store = sessions.NewCookieStore(hash.Sum(nil))
	application.Store.Options = &sessions.Options{
		Path:     "/",
		HttpOnly: true,
		Secure:   cookieSecure,
	}

	application.DbMap = models.GetDbMap(
		APISecret,
		baseURL,
		DBUser,
		DBPassword,
		DBHost,
		DBPort,
		DBName)

	application.CsrfProtection = &CsrfProtection{
		Key:    CSRFKey,
		Cookie: CSRFCookie,
		Header: CSRFHeader,
		Secure: cookieSecure,
	}

	application.APISecret = APISecret
}

func (application *Application) LoadTemplates(templatePath string) error {
	var templates []string

	fn := func(path string, f os.FileInfo, err error) error {
		// If path doesn't exist, or other error with path, return error so that
		// Walk will quit and return the error to the caller.
		if err != nil {
			return err
		}
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".html") {
			templates = append(templates, path)
		}
		return nil
	}

	err := filepath.Walk(templatePath, fn)
	if err != nil {
		return err
	}

	// Since template.Must panics with non-nil error, it is much more
	// informative to pass the error to the caller (runMain) to log it and exit
	// gracefully.
	httpTemplates, err := template.ParseFiles(templates...)
	if err != nil {
		return err
	}

	application.Template = template.Must(httpTemplates, nil)
	application.TemplatesPath = templatePath
	return nil
}

func (application *Application) Close() {
	log.Info("Application.Close() called")
}

func (application *Application) Route(controller interface{}, route string) web.HandlerFunc {
	return func(c web.C, w http.ResponseWriter, r *http.Request) {
		c.Env["Content-Type"] = "text/html"

		// TODO: nuke Route and get rid of this reflect usage.
		methodValue := reflect.ValueOf(controller).MethodByName(route)
		methodInterface := methodValue.Interface()
		method := methodInterface.(func(c web.C, r *http.Request) (string, int))

		body, code := method(c, r)

		// Save the session in c.Env["Session"].
		if err := saveSession(c, w, r); err != nil {
			log.Errorf("Can't save session: %v", err)
		}

		switch code {
		case http.StatusOK, http.StatusProcessing, http.StatusServiceUnavailable:
			if _, exists := c.Env["Content-Type"]; exists {
				w.Header().Set("Content-Type", c.Env["Content-Type"].(string))
			}
			w.WriteHeader(code)
			io.WriteString(w, body)
		case http.StatusSeeOther, http.StatusFound:
			http.Redirect(w, r, body, code)
		case http.StatusUnauthorized:
			http.Error(w, http.StatusText(403), 403)
		case http.StatusInternalServerError:
			http.Error(w, http.StatusText(500), 500)
		}
	}
}

func saveSession(c web.C, w http.ResponseWriter, r *http.Request) error {
	if session, exists := c.Env["Session"]; exists {
		return session.(*sessions.Session).Save(r, w)
	}
	return errors.New("Session not available")
}

// APIHandler executes an API processing function that provides an *APIResponse
// required by WriteAPIResponse.  It returns an web.HandlerFunc so it can be
// used with a goji router.
func (application *Application) APIHandler(apiFun func(web.C, *http.Request) *APIResponse) web.HandlerFunc {
	return func(c web.C, w http.ResponseWriter, r *http.Request) {
		apiResp := apiFun(c, r)

		if err := saveSession(c, w, r); err != nil {
			log.Errorf("Can't save session: %v", err)
		}

		if apiResp != nil {
			WriteAPIResponse(apiResp, http.StatusOK, w)
			return
		}

		APIInvalidHandler(w, r)
	}
}

// WriteAPIResponse marshals the given poolapi.Response into the
// http.ResponseWriter and sets HTTP status code.
func WriteAPIResponse(resp *APIResponse, code int, w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Warnf("JSON encode error: %v", err)
	}
}

// APIInvalidHandler responds to invalid requests. It matches the signature of
// http.HanderFunc.
func APIInvalidHandler(w http.ResponseWriter, _ *http.Request) {
	resp := &APIResponse{Status: "error",
		Code:    codes.InvalidArgument,
		Message: "invalid API command or version",
	}
	WriteAPIResponse(resp, http.StatusNotFound, w)
}

// APIResponse is the response struct used by the server to marshal to a JSON
// object. Data should be another struct with JSON tags.
type APIResponse struct {
	Status  string      `json:"status"`
	Code    codes.Code  `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// NewAPIResponse is a constructor for APIResponse.
func NewAPIResponse(status string, code codes.Code, message string, data interface{}) *APIResponse {
	return &APIResponse{status, code, message, data}
}
