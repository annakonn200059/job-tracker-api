package auth_api

import (
	"net"
	"net/http"
	"time"

	users_models "github.com/annakonn200059/job-tracker-api/domains/users"
	apihttp "github.com/annakonn200059/job-tracker-api/http"
	auth_service "github.com/annakonn200059/job-tracker-api/services/auth"
)

type Handler struct {
	svc    *auth_service.Service
	cookie apihttp.CookieConfig
}

func NewHandler(svc *auth_service.Service, cookie apihttp.CookieConfig) *Handler {
	return &Handler{svc: svc, cookie: cookie}
}

// Register mounts the auth routes on mux. POST /auth/google is only mounted
// when a Google client ID is configured.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/register", h.register)
	mux.HandleFunc("POST /auth/login", h.login)
	mux.HandleFunc("POST /auth/logout", h.logout)
	mux.HandleFunc("GET /auth/me", h.me)
	if h.svc.GoogleEnabled() {
		mux.HandleFunc("POST /auth/google", h.google)
	}
}

// ------------------------------------------------------------------ wire types

type registerRequest struct {
	Email       string  `json:"email"`
	Password    string  `json:"password"`
	DisplayName *string `json:"display_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type googleRequest struct {
	// IDToken is the "credential" field Google Identity Services hands the
	// frontend after the user picks an account.
	IDToken string `json:"id_token"`
}

type userResponse struct {
	ID            int64   `json:"id"`
	Email         string  `json:"email"`
	DisplayName   *string `json:"display_name"`
	Locale        string  `json:"locale"`
	EmailVerified bool    `json:"email_verified"`
	HasPassword   bool    `json:"has_password"`
}

// sessionResponse carries the token as well as setting the cookie, so a
// non-browser client (or a Next.js server component forwarding requests)
// can use "Authorization: Bearer". Browser code should rely on the
// HttpOnly cookie and not store the token itself.
type sessionResponse struct {
	User      userResponse `json:"user"`
	Token     string       `json:"token"`
	ExpiresAt time.Time    `json:"expires_at"`
}

func toUserResponse(u *users_models.User) userResponse {
	return userResponse{
		ID:            u.ID,
		Email:         u.Email,
		DisplayName:   u.DisplayName,
		Locale:        u.Locale,
		EmailVerified: u.EmailVerifiedAt != nil,
		HasPassword:   u.PasswordHash != nil,
	}
}

// ------------------------------------------------------------------ handlers

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := apihttp.DecodeJSON(w, r, &req); err != nil {
		apihttp.WriteError(w, r, err)
		return
	}
	res, err := h.svc.Register(r.Context(), auth_service.RegisterParams{
		Email:       req.Email,
		Password:    req.Password,
		DisplayName: req.DisplayName,
	}, clientMeta(r))
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}
	h.writeSession(w, http.StatusCreated, res)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := apihttp.DecodeJSON(w, r, &req); err != nil {
		apihttp.WriteError(w, r, err)
		return
	}
	res, err := h.svc.Login(r.Context(), req.Email, req.Password, clientMeta(r))
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}
	h.writeSession(w, http.StatusOK, res)
}

func (h *Handler) google(w http.ResponseWriter, r *http.Request) {
	var req googleRequest
	if err := apihttp.DecodeJSON(w, r, &req); err != nil {
		apihttp.WriteError(w, r, err)
		return
	}
	res, err := h.svc.LoginWithGoogle(r.Context(), req.IDToken, clientMeta(r))
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}
	h.writeSession(w, http.StatusOK, res)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Logout(r.Context(), apihttp.SessionToken(r)); err != nil {
		apihttp.WriteError(w, r, err)
		return
	}
	apihttp.ClearSessionCookie(w, h.cookie)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	userID, err := apihttp.UserID(r)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}
	u, err := h.svc.Me(r.Context(), userID)
	if err != nil {
		apihttp.WriteError(w, r, err)
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, toUserResponse(u))
}

func (h *Handler) writeSession(w http.ResponseWriter, status int, res *auth_service.Result) {
	apihttp.SetSessionCookie(w, h.cookie, res.Token, res.ExpiresAt)
	apihttp.WriteJSON(w, status, sessionResponse{
		User:      toUserResponse(res.User),
		Token:     res.Token,
		ExpiresAt: res.ExpiresAt,
	})
}

// clientMeta is informational only (shown to the user, never trusted), so
// RemoteAddr is fine even behind a proxy.
func clientMeta(r *http.Request) auth_service.ClientMeta {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	return auth_service.ClientMeta{UserAgent: r.UserAgent(), IP: ip}
}
