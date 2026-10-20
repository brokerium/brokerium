// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/brokerium/brokerium/internal/edition"
	"github.com/brokerium/brokerium/internal/store"
)

func (s *Server) registerLicense() {
	s.Route("GET /api/v1/license", store.RoleViewer, s.handleLicense)
	s.Route("PUT /api/v1/license", store.RoleAdmin, s.handleSetLicense)
	s.Route("DELETE /api/v1/license", store.RoleAdmin, s.handleDeleteLicense)
}

// FeatureView is an Enterprise feature and whether the license covers it.
type FeatureView struct {
	edition.Feature
	Licensed bool `json:"licensed"`
}

// LicenseView is the edition, the Enterprise features and the license of the server.
type LicenseView struct {
	Edition  string               `json:"edition"`
	Features []FeatureView        `json:"features"`
	License  *edition.LicenseInfo `json:"license"`
}

func licenseView() LicenseView {
	m := edition.CurrentManager()
	v := LicenseView{Edition: edition.Name, Features: make([]FeatureView, 0, len(edition.Catalog))}
	for _, f := range edition.Catalog {
		v.Features = append(v.Features, FeatureView{Feature: f, Licensed: m != nil && m.Allows(f.ID)})
	}
	if m != nil {
		v.License = m.Info()
	}
	return v
}

func (s *Server) handleLicense(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, licenseView())
}

type licenseRequest struct {
	Key string `json:"key"`
}

func (s *Server) handleSetLicense(w http.ResponseWriter, r *http.Request) {
	m := licenseManager(w)
	if m == nil {
		return
	}
	var req licenseRequest
	if !Decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Key) == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "key is required")
		return
	}
	info, err := m.Set(r.Context(), req.Key, UserFrom(r.Context()).Username)
	var target string
	var detail any
	if info != nil {
		target = info.ID
		detail = map[string]any{"customer": info.Customer, "features": info.Features, "expiresAt": info.ExpiresAt}
	}
	s.Audit(r, "license.update", "", target, detail, err)
	if err != nil {
		writeLicenseError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, licenseView())
}

func (s *Server) handleDeleteLicense(w http.ResponseWriter, r *http.Request) {
	m := licenseManager(w)
	if m == nil {
		return
	}
	var target string
	if info := m.Info(); info != nil {
		target = info.ID
	}
	err := m.Clear(r.Context(), UserFrom(r.Context()).Username)
	s.Audit(r, "license.delete", "", target, nil, err)
	if err != nil {
		writeLicenseError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// licenseManager returns the license manager, or answers 409 edition_required in Community
// builds.
func licenseManager(w http.ResponseWriter) edition.Manager {
	m := edition.CurrentManager()
	if m == nil {
		WriteError(w, http.StatusConflict, "edition_required",
			"license keys need Brokerium Enterprise; this server runs the Community edition")
	}
	return m
}

func writeLicenseError(w http.ResponseWriter, err error) {
	var external *edition.ExternalKeyError
	switch {
	case errors.As(err, &external):
		WriteError(w, http.StatusConflict, "license_managed_externally", err.Error())
	case errors.Is(err, edition.ErrInvalidKey):
		WriteError(w, http.StatusBadRequest, "invalid_license", err.Error())
	default:
		WriteError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}
