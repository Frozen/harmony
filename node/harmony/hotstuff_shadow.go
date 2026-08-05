package node

import (
	"context"
	"errors"
	"reflect"

	"github.com/harmony-one/harmony/consensus/hotstuff"
	hshotstuff "github.com/harmony-one/harmony/consensus/hotstuff/harmony"
)

var (
	ErrHotStuffShadowDisabled       = errors.New("hotstuff shadow ingress is disabled")
	ErrInvalidHotStuffShadowHandler = errors.New("hotstuff shadow handler is invalid")
)

// HotStuffShadowHandler is the feature-gated network boundary for shadow-only
// HotStuff processing. Implementations may observe commits but must not write
// blocks or canonical finality state.
type HotStuffShadowHandler interface {
	Domain() hotstuff.VoteDomain
	Handle(context.Context, hshotstuff.WireMessage) error
}

type hotStuffShadowRegistration struct {
	handler HotStuffShadowHandler
	domain  hotstuff.VoteDomain
}

// SetHotStuffShadowHandler installs the shadow ingress before StartPubSub.
// Passing nil explicitly disables the feature; typed-nil handlers are rejected.
func (node *Node) SetHotStuffShadowHandler(handler HotStuffShadowHandler) error {
	if isTypedNilHotStuffShadowHandler(handler) {
		return ErrInvalidHotStuffShadowHandler
	}
	var registration *hotStuffShadowRegistration
	if handler != nil {
		registration = &hotStuffShadowRegistration{
			handler: handler,
			domain:  handler.Domain(),
		}
	}
	node.hotStuffShadowMu.Lock()
	defer node.hotStuffShadowMu.Unlock()
	node.hotStuffShadowHandler = registration
	return nil
}

func (node *Node) validateHotStuffShadowMessage(
	message []byte,
) (HotStuffShadowHandler, hshotstuff.WireMessage, error) {
	node.hotStuffShadowMu.RLock()
	registration := node.hotStuffShadowHandler
	node.hotStuffShadowMu.RUnlock()
	if registration == nil {
		return nil, hshotstuff.WireMessage{}, ErrHotStuffShadowDisabled
	}
	decoded, err := hshotstuff.DecodeWireMessageForDomain(message, registration.domain)
	if err != nil {
		return nil, hshotstuff.WireMessage{}, err
	}
	return registration.handler, decoded, nil
}

func (node *Node) validateHotStuffShadowTopicMessage(
	message []byte,
	consensusBound bool,
) (HotStuffShadowHandler, hshotstuff.WireMessage, error) {
	if !consensusBound {
		return nil, hshotstuff.WireMessage{}, errConsensusMessageOnUnexpectedTopic
	}
	return node.validateHotStuffShadowMessage(message)
}

func isTypedNilHotStuffShadowHandler(handler HotStuffShadowHandler) bool {
	if handler == nil {
		return false
	}
	value := reflect.ValueOf(handler)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
