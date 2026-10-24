package api

import (
	"net/http"

	"patiklinik/internal/httpx"
)

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (a *API) login(w http.ResponseWriter, r *http.Request) error {
	var req loginReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	v := httpx.Validation{}
	if trim(req.Email) == "" {
		v.Add("email", "required")
	}
	if req.Password == "" {
		v.Add("password", "required")
	}
	if err := v.Err(); err != nil {
		return err
	}
	tokens, err := a.Auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, tokens)
	return nil
}

type refreshReq struct {
	RefreshToken string `json:"refresh_token"`
}

func (a *API) refresh(w http.ResponseWriter, r *http.Request) error {
	var req refreshReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.RefreshToken == "" {
		return httpx.Validation{"refresh_token": "required"}.Err()
	}
	tokens, err := a.Auth.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, tokens)
	return nil
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) error {
	var req refreshReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := a.Auth.Logout(r.Context(), req.RefreshToken); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) me(w http.ResponseWriter, r *http.Request) error {
	httpx.WriteJSON(w, http.StatusOK, principal(r))
	return nil
}
