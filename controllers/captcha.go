// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package controllers

import (
	"image/png"
	"net/http"
	"path"
	"strings"

	"github.com/chappjc/captcha"
	"github.com/zenazn/goji/web"
)

type CaptchaHandler struct {
	ImgWidth  int
	ImgHeight int
	Opts      *captcha.DistortionOpts
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

	if r.FormValue("reload") != "" && !captcha.Reload(id) {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// Retrieve the digits for this captcha id from the global store.
	digits := captcha.Digits(id)
	if len(digits) == 0 {
		http.NotFound(w, r)
		return
	}

	// Generate the image for the digits.
	ch := controller.captchaHandler
	img := captcha.NewImage(id, digits, ch.ImgWidth, ch.ImgHeight, ch.Opts)
	if img == nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError)
		return
	}

	// Encode the image as a PNG directly to the ResponseWriter.
	enc := png.Encoder{
		CompressionLevel: png.BestSpeed,
	}
	if err := enc.Encode(w, img.Paletted); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError)
	}
}

func (controller *MainController) CaptchaVerify(c web.C, w http.ResponseWriter, r *http.Request) {
	id, solution := r.FormValue("captchaId"), r.FormValue("captchaSolution")
	if id == "" {
		http.Error(w, "invalid captcha id", http.StatusBadRequest)
		return
	}

	session := controller.GetSession(c)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if captcha.VerifyString(id, solution) {
		session.Values["CaptchaDone"] = true
	} else {
		session.Values["CaptchaDone"] = false
		session.AddFlash("Captcha verification failed. Please try again.",
			"captchaFailed")
	}

	if err := session.Save(r, w); err != nil {
		log.Criticalf("session.Save() failed: %v", err)
		http.Error(w, "failed to save session", http.StatusInternalServerError)
	}

	ref := r.Referer()
	if ref == "" {
		ref = "/"
	}
	http.Redirect(w, r, ref, http.StatusFound)
}
