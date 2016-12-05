package system

import (
	"crypto/sha256"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/decred/dcrstakepool/models"
	"github.com/gorilla/sessions"
	"github.com/zenazn/goji/web"
	"gopkg.in/gorp.v1"
)

// CSRF token constants
const (
	CSRFCookie = "XSRF-TOKEN"
	CSRFHeader = "X-XSRF-TOKEN"
	CSRFKey    = "csrf_token"
)

type CsrfProtection struct {
	Key    string
	Cookie string
	Header string
	Secure bool
}

type Application struct {
	Template       *template.Template
	Store          *sessions.CookieStore
	DbMap          *gorp.DbMap
	CsrfProtection *CsrfProtection
}

// GojiWebHandlerFunc is an adaptor that allows an http.HanderFunc where a
// web.HandlerFunc is required.
func GojiWebHandlerFunc(h http.HandlerFunc) web.HandlerFunc {
	return func(_ web.C, w http.ResponseWriter, r *http.Request) {
		h(w, r)
	}
}

func (application *Application) Init(cookieSecret string, cookieSecure bool,
	DBHost string, DBName string, DBPassword string, DBPort string,
	DBUser string) {

	hash := sha256.New()
	io.WriteString(hash, cookieSecret)
	application.Store = sessions.NewCookieStore(hash.Sum(nil))
	application.Store.Options = &sessions.Options{
		Path:     "/",
		HttpOnly: true,
		Secure:   cookieSecure,
	}

	application.DbMap = models.GetDbMap(
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
}

func (application *Application) LoadTemplates(templatePath string) error {
	var templates []string

	fn := func(path string, f os.FileInfo, err error) error {
		if f.IsDir() != true && strings.HasSuffix(f.Name(), ".html") {
			templates = append(templates, path)
		}
		return nil
	}

	err := filepath.Walk(templatePath, fn)

	if err != nil {
		return err
	}

	application.Template = template.Must(template.ParseFiles(templates...))
	return nil
}

func (application *Application) Close() {
	log.Info("Application.Close() called")
}

func (application *Application) Route(controller interface{}, route string) web.HandlerFunc {
	fn := func(c web.C, w http.ResponseWriter, r *http.Request) {
		c.Env["Content-Type"] = "text/html"

		methodValue := reflect.ValueOf(controller).MethodByName(route)
		methodInterface := methodValue.Interface()
		method := methodInterface.(func(c web.C, r *http.Request) (string, int))

		body, code := method(c, r)

		if err := saveSession(c, w, r); err != nil {
			log.Errorf("Can't save session: %v", err)
		}

		if respHeader, exists := c.Env["ResponseHeaderMap"]; exists {
			if hdrMap, ok := respHeader.(map[string]string); ok {
				for key, val := range hdrMap {
					w.Header().Set(key, val)
				}
			}
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
		case http.StatusInternalServerError:
			http.Error(w, http.StatusText(500), 500)
		}
	}
	return fn
}

func saveSession(c web.C, w http.ResponseWriter, r *http.Request) error {
	if session, exists := c.Env["Session"]; exists {
		return session.(*sessions.Session).Save(r, w)
	}
	return log.Errorf("Session not available")
}

func (application *Application) APIHandler(apiFun func(web.C, *http.Request) *APIResponse) web.HandlerFunc {
	return func(c web.C, w http.ResponseWriter, r *http.Request) {
		if err := saveSession(c, w, r); err != nil {
			log.Errorf("Can't save session: %v", err)
		}

		apiResp := apiFun(c, r)
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
		Message: "invalid API version",
	}
	WriteAPIResponse(resp, http.StatusNotFound, w)
}

type APIResponse struct {
	Status  string      `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func NewAPIResponse(status, message string, data interface{}) *APIResponse {
	return &APIResponse{status, message, data}
}
