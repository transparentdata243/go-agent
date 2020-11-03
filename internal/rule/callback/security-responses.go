// Copyright (c) 2016 - 2020 Sqreen. All Rights Reserved.
// Please refer to our terms for more information:
// https://www.sqreen.io/terms.html

//sqreen:ignore

package callback

import (
	"encoding/json"
	"net"
	"net/http"

	"github.com/sqreen/go-agent/internal/actor"
	"github.com/sqreen/go-agent/internal/backend/api"
	protection_context "github.com/sqreen/go-agent/internal/protection/context"
	http_protection "github.com/sqreen/go-agent/internal/protection/http"
	"github.com/sqreen/go-agent/internal/sqlib/sqassert"
	"github.com/sqreen/go-agent/internal/sqlib/sqerrors"
	"github.com/sqreen/go-agent/internal/sqlib/sqhook"
	"github.com/sqreen/go-agent/sdk/types"
)

// Event names
const (
	blockIPEventName      = "app.sqreen.action.block_ip"
	blockUserEventName    = "app.sqreen.action.block_user"
	redirectIPEventName   = "app.sqreen.action.redirect_ip"
	redirectUserEventName = "app.sqreen.action.redirect_user"
)

func NewIPSecurityResponseCallback(r RuleContext, _ NativeCallbackConfig) (sqhook.PrologCallback, error) {
	return newIPSecurityResponsePrologCallback(r), nil
}

type securityResponseError struct{}

func (securityResponseError) Error() string { return "aborted by a security response" }

func newIPSecurityResponsePrologCallback(r RuleContext) http_protection.BlockingPrologCallbackType {
	return func(ctx **http_protection.ProtectionContext) (epilog http_protection.BlockingEpilogCallbackType, prologErr error) {
		r.Pre(func(c CallbackContext) error {
			ctx := *ctx
			sqassert.NotNil(ctx)
			ip := c.ProtectionContext().ClientIP()
			action, exists, err := ctx.FindActionByIP(ip)
			if err != nil {
				type errKey struct{}
				return sqerrors.WithKey(sqerrors.Wrapf(err, "unexpected error while searching IP address `%#+v` in the IP action data structure", ip), errKey{})
			}
			if !exists {
				return nil
			}

			epilog = func(e *error) {
				writeIPSecurityResponse(ctx, action, ip)
				*e = types.SqreenError{Err: securityResponseError{}}
			}
			prologErr = nil
			return nil
		})

		return
	}
}

// The security responses are a bit weird as they allow to customize the
// response only for redirection, otherwise it blocks with the global blocking
// settings. The contract with the HTTP protection here is to use a distinct
// error value according to what we need.
func writeIPSecurityResponse(ctx *http_protection.ProtectionContext, action actor.Action, ip net.IP) {
	var properties protection_context.EventProperties
	if redirect, ok := action.(actor.RedirectAction); ok {
		properties = newRedirectedIPEventProperties(redirect, ip)
		ctx.TrackEvent(redirectIPEventName).WithProperties(properties)
		writeRedirectionResponse(ctx.ResponseWriter, redirect.RedirectionURL())
	} else {
		properties = newBlockedIPEventProperties(action, ip)
		ctx.TrackEvent(blockIPEventName).WithProperties(properties)
		ctx.WriteDefaultBlockingResponse()
	}
}

func NewUserSecurityResponseCallback(r RuleContext, _ NativeCallbackConfig) (sqhook.PrologCallback, error) {
	return newUserSecurityResponsePrologCallback(r), nil
}

func newUserSecurityResponsePrologCallback(r RuleContext) http_protection.IdentifyUserPrologCallbackType {
	return func(ctx **http_protection.ProtectionContext, uid *map[string]string) (epilog http_protection.BlockingEpilogCallbackType, prologErr error) {
		r.Pre(func(c CallbackContext) error {
			sqassert.NotNil(ctx)
			p := *ctx

			id := *uid
			action, exists := p.FindActionByUserID(id)
			if !exists {
				return nil
			}

			epilog = func(e *error) {
				writeUserSecurityResponse(p, action, id)
				*e = types.SqreenError{Err: securityResponseError{}}
			}
			prologErr = nil
			return nil
		})

		return
	}
}

func writeUserSecurityResponse(p *http_protection.ProtectionContext, action actor.Action, userID map[string]string) {
	// Since this call happens in the handler, we need to close its context
	// which also let know the HTTP protection layer that it shouldn't continue
	// with post-handler protections.
	var properties protection_context.EventProperties
	if redirect, ok := action.(actor.RedirectAction); ok {
		p.HandleAttack(false, nil)
		properties = newRedirectedUserEventProperties(redirect, userID)
		p.TrackEvent(redirectUserEventName).WithProperties(properties)
		writeRedirectionResponse(p.ResponseWriter, redirect.RedirectionURL())
	} else {
		properties = newBlockedUserEventProperties(action, userID)
		p.TrackEvent(blockUserEventName).WithProperties(properties)
		p.HandleAttack(true, nil)
	}
}

func writeRedirectionResponse(w http.ResponseWriter, location string) {
	w.Header().Set("Location", location)
	w.WriteHeader(http.StatusSeeOther)
}

// blockedIPEventProperties implements `types.EventProperties` to be marshaled
// to an SDK event property structure.
type blockedIPEventProperties struct {
	action actor.Action
	ip     net.IP
}

func newBlockedIPEventProperties(action actor.Action, ip net.IP) *blockedIPEventProperties {
	return &blockedIPEventProperties{
		action: action,
		ip:     ip,
	}
}
func (p *blockedIPEventProperties) MarshalJSON() ([]byte, error) {
	pb := api.NewBlockedIPEventPropertiesFromFace(p)
	return json.Marshal(pb)
}
func (p *blockedIPEventProperties) GetActionId() string {
	return p.action.ActionID()
}
func (p *blockedIPEventProperties) GetOutput() api.BlockedIPEventProperties_Output {
	return *api.NewBlockedIPEventProperties_OutputFromFace(p)
}
func (p *blockedIPEventProperties) GetIpAddress() string {
	return p.ip.String()
}

// blockedUserEventProperties implements `types.EventProperties` to be marshaled
// to an SDK event property structure.
type blockedUserEventProperties struct {
	action actor.Action
	userID map[string]string
}

func newBlockedUserEventProperties(action actor.Action, userID map[string]string) *blockedUserEventProperties {
	return &blockedUserEventProperties{
		action: action,
		userID: userID,
	}
}
func (p *blockedUserEventProperties) MarshalJSON() ([]byte, error) {
	pb := api.NewBlockedUserEventPropertiesFromFace(p)
	return json.Marshal(pb)
}
func (p *blockedUserEventProperties) GetActionId() string {
	return p.action.ActionID()
}
func (p *blockedUserEventProperties) GetOutput() api.BlockedUserEventPropertiesOutput {
	return *api.NewBlockedUserEventPropertiesOutputFromFace(p)
}
func (p *blockedUserEventProperties) GetUser() map[string]string {
	return p.userID
}

// redirectedUserEventProperties implements `types.EventProperties` to be marshaled
// to an SDK event property structure.
type redirectedUserEventProperties struct {
	action actor.Action
	userID map[string]string
}

func newRedirectedUserEventProperties(action actor.Action, userID map[string]string) *redirectedUserEventProperties {
	return &redirectedUserEventProperties{
		action: action,
		userID: userID,
	}
}
func (p *redirectedUserEventProperties) MarshalJSON() ([]byte, error) {
	pb := api.NewRedirectedUserEventPropertiesFromFace(p)
	return json.Marshal(pb)
}
func (p *redirectedUserEventProperties) GetActionId() string {
	return p.action.ActionID()
}
func (p *redirectedUserEventProperties) GetOutput() api.RedirectedUserEventPropertiesOutput {
	return *api.NewRedirectedUserEventPropertiesOutputFromFace(p)
}
func (p *redirectedUserEventProperties) GetUser() map[string]string {
	return p.userID
}

// redirectedIPEventProperties implements `types.EventProperties` to be marshaled
// to an SDK event property structure.
type redirectedIPEventProperties struct {
	action actor.RedirectAction
	ip     net.IP
}

func newRedirectedIPEventProperties(action actor.RedirectAction, ip net.IP) *redirectedIPEventProperties {
	return &redirectedIPEventProperties{
		action: action,
		ip:     ip,
	}
}
func (p *redirectedIPEventProperties) MarshalJSON() ([]byte, error) {
	pb := api.NewRedirectedIPEventPropertiesFromFace(p)
	return json.Marshal(pb)
}
func (p *redirectedIPEventProperties) GetActionId() string {
	return p.action.ActionID()
}
func (p *redirectedIPEventProperties) GetOutput() api.RedirectedIPEventPropertiesOutput {
	return *api.NewRedirectedIPEventPropertiesOutputFromFace(p)
}
func (p *redirectedIPEventProperties) GetIpAddress() string {
	return p.ip.String()
}
func (p *redirectedIPEventProperties) GetURL() string {
	return p.action.RedirectionURL()
}

// SecurityResponseMatch is an error type wrapping the security response that
// matched the request and helping in bubbling up to Sqreen's middleware
// function to abort the request.
type SecurityResponseMatch struct{}

func (SecurityResponseMatch) Error() string {
	return "a security response matched the request"
}
