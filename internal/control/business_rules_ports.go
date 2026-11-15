package control

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"wangshangliao/internal/database"
	"wangshangliao/internal/features"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

const (
	businessWorkers     = 4
	businessWaiting     = 64
	businessTenantWork  = 2
	businessTenantWait  = 8
	businessMessagePage = 100
	businessGroupPage   = 10
	businessRoleReads   = 2
	businessJobLifetime = 120 * time.Second
	businessWriteFence  = 8 * time.Second
	businessWriteGap    = 5 * time.Second
)

type BusinessScope struct {
	controlledExecutionFence
	Platform, Tenant, Actor, Group, NativeGroup, Account, NativeSelf string
	BindingVersion, BindingEpoch, AccountEpoch, AccountVersion       int64
	RuntimeVersion, ActorVersion, SubscriptionVersion, PlanVersion   int64
	ReceiverVersion, DeploymentVersion                               int64
	CapabilityVersion                                                int64
	ReceiverOwner                                                    string
	EnabledAt                                                        time.Time
	Authorization                                                    []string
	AdmissionVersions                                                []string
}

type BusinessMessage struct {
	EventID                       int64
	ReceivedAt                    time.Time
	Group, Sender, Text, ClientID string
	Reference                     *nativeauth.MessageReference
	Challenge                     bool
}

// BusinessEvidence belongs to this native read, not to a cached grant. The
// transport port supplies it again immediately before the final write.
type BusinessEvidence struct {
	Platform, Account, Group, SelfID, Target string
	OwnerID                                  string
	SelfRole, TargetRole                     string
	AccountEpoch, AccountVersion             int64
	DeploymentVersion                        int64
	CredentialDigest                         [32]byte
	ObservedAt                               time.Time
}

// BusinessAttempt is created from a sealed durable rule job. Its identity and
// encryption counters are persisted before the Execute port is invoked.
type BusinessAttempt struct {
	JobID, ClientID, Action, Target, Text string
	Sender                                string
	Scope                                 BusinessScope
	Minutes                               int
	Reference                             *nativeauth.MessageReference
	Owner                                 string
	Revision                              int64
	Nonce, AAD                            uint64
	MessageAt                             time.Time
	RecordRoute                           func(context.Context, nativewire.TextRoute) error
}

// BusinessFence rechecks live authorization and new role evidence and permits
// one bounded native write. The port must never invoke write outside this fence.
type BusinessFence func(context.Context, BusinessEvidence, func(context.Context) error) error
type BusinessExecutePort func(context.Context, BusinessAttempt, BusinessFence) (confirmed bool, err error)

type BusinessRulesService struct {
	DB           *database.DB
	Vault        *security.Vault
	Catalog      []PermissionSpec
	Conversation func(context.Context, BusinessScope) (string, error)
	ReadSource   func(BusinessScope, string, int64, []byte, []byte) (BusinessMessage, error)
	FreshRole    func(context.Context, BusinessScope, string) (BusinessEvidence, error)
	Execute      BusinessExecutePort
	// Observe may only read the original attempt; false leaves it unknown.
	Observe func(context.Context, BusinessAttempt) (bool, error)

	mu      sync.Mutex
	active  map[string]bool
	running bool
	seen    atomic.Uint64
	queued  atomic.Uint64
	limited atomic.Uint64
	writes  atomic.Uint64
	unknown atomic.Uint64
}

type businessPayload struct {
	Decision  features.BusinessDecision    `json:"decision"`
	Sender    string                       `json:"sender"`
	Reference *nativeauth.MessageReference `json:"reference,omitempty"`
}

type BusinessStats struct {
	Candidates uint64 `json:"candidates"`
	Queued     uint64 `json:"queued"`
	Limited    uint64 `json:"limited"`
	Writes     uint64 `json:"confirmed_writes"`
	Unknown    uint64 `json:"unknown_results"`
	Active     int    `json:"active"`
}

func (s *BusinessRulesService) Snapshot() BusinessStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return BusinessStats{Candidates: s.seen.Load(), Queued: s.queued.Load(),
		Limited: s.limited.Load(), Writes: s.writes.Load(), Unknown: s.unknown.Load(), Active: len(s.active)}
}
