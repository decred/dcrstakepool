package controllers

import (
	"bytes"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/dchest/captcha"
	"github.com/zenazn/goji/web"
)

type CaptchaHandler struct {
	ImgWidth  int
	ImgHeight int
}

func (controller *MainController) CaptchaServe(c web.C, w http.ResponseWriter, r *http.Request) {
	// Get the captcha id by stripping the file extension.
	_, file := path.Split(r.URL.Path)
	ext := path.Ext(file)
	id := strings.TrimSuffix(file, ext)
	if ext != ".png" || id == "" {
		http.NotFound(w, r)
		return
	}

	h := controller.captchaHandler

	if r.FormValue("reload") != "" {
		captcha.Reload(id)
	}

	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	var content bytes.Buffer
	w.Header().Set("Content-Type", "image/png")
	err := captcha.WriteImage(&content, id, h.ImgWidth, h.ImgHeight)
	if err != nil {
		http.Error(w, "failed to generate captcha image", http.StatusInternalServerError)
	}

	http.ServeContent(w, r, id+ext, time.Time{}, bytes.NewReader(content.Bytes()))
}

func (controller *MainController) CaptchaVerify(c web.C, w http.ResponseWriter, r *http.Request) {
	id, solution := r.FormValue("captchaId"), r.FormValue("captchaSolution")
	if id == "" {
		http.Error(w, "invalid captcha id", http.StatusBadRequest)
		return
	}

	session := controller.GetSession(c)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var status int
	if captcha.VerifyString(id, solution) {
		session.Values["CaptchaDone"] = true
		status = http.StatusFound
	} else {
		session.Values["CaptchaDone"] = false
		status = http.StatusBadRequest
	}

	if err := session.Save(r, w); err != nil {
		log.Criticalf("session.Save() failed: %v", err)
		http.Error(w, "failed to save session", http.StatusInternalServerError)
	}

	ref := r.Referer()
	if ref == "" {
		ref = "/"
	}
	http.Redirect(w, r, ref, status)
}
