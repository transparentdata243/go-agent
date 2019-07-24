// Copyright (c) 2016 - 2019 Sqreen. All Rights Reserved.
// Please refer to our terms for more information:
// https://www.sqreen.io/terms.html

package rule_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"encoding/asn1"
	"encoding/base64"
	"math/big"
	"net/http"
	"os"
	"reflect"
	"testing"

	"github.com/sqreen/go-agent/agent/internal/backend/api"
	"github.com/sqreen/go-agent/agent/internal/metrics"
	"github.com/sqreen/go-agent/agent/internal/plog"
	"github.com/sqreen/go-agent/agent/internal/rule"
	"github.com/sqreen/go-agent/agent/sqlib/sqhook"
	"github.com/stretchr/testify/require"
)

func func1(_ http.ResponseWriter, _ *http.Request, _ http.Header, _ int, _ []byte) {}
func func2(_ http.ResponseWriter, _ *http.Request, _ http.Header, _ int, _ []byte) {}

type empty struct{}

func TestEngineUsage(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	publicKey := &privateKey.PublicKey

	logger := plog.NewLogger(plog.Debug, os.Stderr, 0)
	engine := rule.NewEngine(logger, metrics.NewEngine(plog.NewLogger(plog.Debug, os.Stderr, 0), 100000000), publicKey)
	hookFunc1 := sqhook.New(func1)
	require.NotNil(t, hookFunc1)
	hookFunc2 := sqhook.New(func2)
	require.NotNil(t, hookFunc2)

	t.Run("empty state", func(t *testing.T) {
		require.Empty(t, engine.PackID())
		engine.SetRules("my pack id", nil, nil)
		require.Equal(t, engine.PackID(), "my pack id")
		// No problem enabling/disabling the engine
		engine.Enable()
		engine.Disable()
		engine.Enable()
		engine.SetRules("my other pack id", []api.Rule{}, nil)
		require.Equal(t, engine.PackID(), "my other pack id")
	})

	t.Run("multiple rules", func(t *testing.T) {
		engine.Disable()
		engine.SetRules("yet another pack id", []api.Rule{
			{
				Name: "a valid rule",
				Hookpoint: api.Hookpoint{
					Class:    reflect.TypeOf(empty{}).PkgPath(),
					Method:   "func1",
					Callback: "WriteCustomErrorPage",
				},
				Data: api.RuleData{
					Values: []api.RuleDataEntry{
						{&api.CustomErrorPageRuleDataEntry{}},
					},
				},
				Signature: MakeSignature(privateKey, `{"name":"a valid rule"}`),
			},
			{
				Name: "another valid rule",
				Hookpoint: api.Hookpoint{
					Class:    reflect.TypeOf(empty{}).PkgPath(),
					Method:   "func2",
					Callback: "WriteCustomErrorPage",
				},
				Data: api.RuleData{
					Values: []api.RuleDataEntry{
						{&api.CustomErrorPageRuleDataEntry{}},
					},
				},
				Signature: MakeSignature(privateKey, `{"name":"another valid rule"}`),
			},
		}, nil)

		t.Run("callbacks are not attached when disabled", func(t *testing.T) {
			// Check the callbacks were not attached because rules are disabled
			prologFunc1 := hookFunc1.Prolog()
			require.Nil(t, prologFunc1)
			prologFunc2 := hookFunc2.Prolog()
			require.Nil(t, prologFunc2)
		})

		t.Run("enabling the rules attaches the callbacks", func(t *testing.T) {
			// Enable the rules
			engine.Enable()
			// Check the callbacks were now attached
			prologFunc1 := hookFunc1.Prolog()
			require.NotNil(t, prologFunc1)
			prologFunc2 := hookFunc2.Prolog()
			require.NotNil(t, prologFunc2)
		})

		t.Run("disabling the rules removes the callbacks", func(t *testing.T) {
			// Disable the rules
			engine.Disable()
			// Check the callbacks were all removed for func1 and not func2
			prologFunc1 := hookFunc1.Prolog()
			require.Nil(t, prologFunc1)
			prologFunc2 := hookFunc2.Prolog()
			require.Nil(t, prologFunc2)
		})

		t.Run("enabling the rules again sets back the callbacks", func(t *testing.T) {
			// Enable again the rules
			engine.Enable()
			// Check the callbacks are attached again
			prologFunc1 := hookFunc1.Prolog()
			require.NotNil(t, prologFunc1)
			prologFunc2 := hookFunc2.Prolog()
			require.NotNil(t, prologFunc2)
		})
	})

	t.Run("modify enabled rules", func(t *testing.T) {
		// Modify the rules while enabled
		engine.SetRules("a pack id", []api.Rule{
			{
				Name: "another valid rule",
				Hookpoint: api.Hookpoint{
					Class:    reflect.TypeOf(empty{}).PkgPath(),
					Method:   "func2",
					Callback: "WriteCustomErrorPage",
				},
				Data: api.RuleData{
					Values: []api.RuleDataEntry{
						{&api.CustomErrorPageRuleDataEntry{}},
					},
				},
				Signature: MakeSignature(privateKey, `{"name":"another valid rule"}`),
			},
		}, nil)
		// Check the callbacks were removed for func1 and not func2
		prologFunc1 := hookFunc1.Prolog()
		require.Nil(t, prologFunc1)
		prologFunc2 := hookFunc2.Prolog()
		require.NotNil(t, prologFunc2)
	})

	t.Run("replace the enabled rules with an empty array of rules", func(t *testing.T) {
		// Set the rules with an empty array while enabled
		engine.SetRules("yet another pack id", []api.Rule{}, nil)
		// Check the callbacks were all removed for func1 and not func2
		prologFunc1 := hookFunc1.Prolog()
		require.Nil(t, prologFunc1)
		prologFunc2 := hookFunc2.Prolog()
		require.Nil(t, prologFunc2)
	})

	t.Run("add rules with signature issues", func(t *testing.T) {
		validSignature := MakeSignature(privateKey, `{"name":"a valid rule"}`).ECDSASignature

		// Modify the rules while enabled
		engine.SetRules("a pack id", []api.Rule{
			{
				Name: "a valid rule",
				Hookpoint: api.Hookpoint{
					Class:    reflect.TypeOf(empty{}).PkgPath(),
					Method:   "func1",
					Callback: "WriteCustomErrorPage",
				},
				Data: api.RuleData{
					Values: []api.RuleDataEntry{
						{&api.CustomErrorPageRuleDataEntry{}},
					},
				},
				Signature: api.RuleSignature{ /*zero value*/ },
			},
			{
				Name: "a valid rule",
				Hookpoint: api.Hookpoint{
					Class:    reflect.TypeOf(empty{}).PkgPath(),
					Method:   "func1",
					Callback: "WriteCustomErrorPage",
				},
				Data: api.RuleData{
					Values: []api.RuleDataEntry{
						{&api.CustomErrorPageRuleDataEntry{}},
					},
				},
				Signature: api.RuleSignature{
					ECDSASignature: api.ECDSASignature{
						Message: validSignature.Message,
						/* zero signature value */
					},
				},
			},
			{
				Name: "a valid rule",
				Hookpoint: api.Hookpoint{
					Class:    reflect.TypeOf(empty{}).PkgPath(),
					Method:   "func1",
					Callback: "WriteCustomErrorPage",
				},
				Data: api.RuleData{
					Values: []api.RuleDataEntry{
						{&api.CustomErrorPageRuleDataEntry{}},
					},
				},
				Signature: api.RuleSignature{
					ECDSASignature: api.ECDSASignature{
						Value: validSignature.Value,
						/* zero message value */
					},
				},
			},
			{
				Name: "a valid rule",
				Hookpoint: api.Hookpoint{
					Class:    reflect.TypeOf(empty{}).PkgPath(),
					Method:   "func1",
					Callback: "WriteCustomErrorPage",
				},
				Data: api.RuleData{
					Values: []api.RuleDataEntry{
						{&api.CustomErrorPageRuleDataEntry{}},
					},
				},
				Signature: api.RuleSignature{
					ECDSASignature: api.ECDSASignature{
						Value:   validSignature.Value,
						Message: []byte(`wrong message`),
					},
				},
			},
			{
				Name: "a valid rule",
				Hookpoint: api.Hookpoint{
					Class:    reflect.TypeOf(empty{}).PkgPath(),
					Method:   "func1",
					Callback: "WriteCustomErrorPage",
				},
				Data: api.RuleData{
					Values: []api.RuleDataEntry{
						{&api.CustomErrorPageRuleDataEntry{}},
					},
				},
				Signature: api.RuleSignature{
					ECDSASignature: api.ECDSASignature{
						Value:   `wrong value`,
						Message: validSignature.Message,
					},
				},
			},
		}, nil)
		// Check the callbacks were removed for func1 and not func2
		prologFunc1 := hookFunc1.Prolog()
		require.Nil(t, prologFunc1)
	})
}

func MakeSignature(privateKey *ecdsa.PrivateKey, message string) api.RuleSignature {
	hash := sha512.Sum512([]byte(message))
	r, s, err := ecdsa.Sign(rand.Reader, privateKey, hash[:])
	if err != nil {
		panic(err)
	}
	signature, err := asn1.Marshal(struct{ R, S *big.Int }{R: r, S: s})
	if err != nil {
		panic(err)
	}
	return api.RuleSignature{
		ECDSASignature: api.ECDSASignature{
			Message: []byte(message),
			Value:   base64.StdEncoding.EncodeToString(signature),
		},
	}
}