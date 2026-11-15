package gateway

import (
	"context"
	"strconv"
	"time"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

type nativeGroupProof struct {
	group  nativeauth.DirectoryGroup
	roster nativeauth.MemberDirectory
	role   string
	muted  bool
	at     time.Time
}

// proveNativeGroup reads complete current evidence. It never promotes directory
// projections into grants, and it does not run on the receive/ACK goroutine.
func proveNativeGroup(ctx context.Context, client textReadClient, connection *ownedTextConnection, group string) (nativeGroupProof, error) {
	var out nativeGroupProof
	// Timestamp the start, not completion: slow pagination must not make an
	// old self-role observation appear newly sampled at the final page.
	out.at = time.Now().UTC()
	groups, err := client.Directory(ctx, connection.uid)
	if err != nil {
		return out, err
	}
	for _, item := range groups {
		if item.ID == group {
			if out.group.ID != "" {
				return out, ErrTextRoute
			}
			out.group = item
		}
	}
	if out.group.Team == "" {
		return out, ErrTextRoute
	}
	out.roster, err = client.Members(ctx, group)
	if err != nil {
		return out, err
	}
	if !out.roster.Complete || out.roster.Group != group {
		return out, ErrTextRoute
	}
	for _, member := range out.roster.Members {
		if member.ID == strconv.FormatUint(connection.uid, 10) {
			if out.role != "" || member.Transport != connection.sender ||
				(member.Role != "admin" && member.Role != "owner" && member.Role != "member") {
				return out, ErrTextRoute
			}
			out.role = member.Role
		}
	}
	if out.role == "" {
		return out, ErrTextRoute
	}
	body, err := nativewire.MutedMembersRequest(out.group.Team)
	if err != nil {
		return out, ErrTextRoute
	}
	reply, err := connection.transport.request(ctx, 8, 27, body)
	if err != nil {
		return out, err
	}
	if reply.Code != 200 {
		return out, ErrTextRoute
	}
	muted, err := nativewire.ParseMutedMembers(reply.Body, out.group.Team)
	// Mapping must be complete even when this bot is muted. Collecting a
	// negative observation is allowed, submitting text is not.
	if err != nil || verifyTextMuteRoster(out.roster, muted, "") != nil {
		return out, ErrTextRoute
	}
	out.muted = muted[connection.sender]
	if time.Since(out.at) > 5*time.Second {
		return out, ErrTextRoute
	}
	return out, nil
}

// ObserveRole reuses the current connection and read lane. It never logs in,
// starts a new connection, issues a claim code or invokes a native mutation.
func (m *ReceiverManager) ObserveRole(ctx context.Context, account, group string) (nativewire.RoleEvidence, error) {
	return m.observeRole(ctx, account, group, func(d nativeauth.Deployment, session nativeauth.Session, ids nativeauth.RequestIDSource) (textReadClient, error) {
		return nativeauth.Resume(d, session, ids)
	})
}

func (m *ReceiverManager) observeRole(ctx context.Context, account, group string,
	factory func(nativeauth.Deployment, nativeauth.Session, nativeauth.RequestIDSource) (textReadClient, error)) (nativewire.RoleEvidence, error) {
	m.mu.Lock()
	w := m.jobs[account]
	if w == nil {
		m.mu.Unlock()
		return nativewire.RoleEvidence{}, ErrTextOwner
	}
	i := TextIntent{AccountID: account, ReceiverOwner: m.owner, AccountEpoch: w.spec.Epoch, Revision: w.spec.Revision,
		Message: nativewire.GroupText{SenderID: 0}}
	w.mu.Lock()
	if w.connection != nil {
		i.Message.SenderID = w.connection.uid
	}
	w.mu.Unlock()
	m.mu.Unlock()
	_, connection, err := m.textOwner(i)
	if err != nil {
		return nativewire.RoleEvidence{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(connection.ctx, cancel)
	defer func() { stop(); cancel() }()
	value, err := m.readValue(ctx, account, "role-proof:"+strconv.FormatInt(i.AccountEpoch, 10)+":"+
		strconv.FormatInt(i.Revision, 10)+":"+group, ReadRefresh, func(work context.Context) (any, error) {
		d, saved, _, _, err := m.load(work, w.spec)
		if err != nil || saved.DeploymentVersion != connection.version ||
			saved.Session.UID != connection.uid || saved.Session.NIMID != connection.sender {
			return nil, ErrTextOwner
		}
		var ids nativeauth.RequestIDSource
		if m.IDs != nil {
			ids = m.IDs(saved.DeploymentVersion)
		}
		client, err := factory(d, saved.Session, ids)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		proof, err := proveNativeGroup(work, client, connection, group)
		if err != nil {
			return nil, err
		}
		if _, current, err := m.textOwner(i); err != nil || current != connection {
			return nil, ErrTextOwner
		}
		return nativewire.RoleEvidence{AccountID: account, ReceiverOwner: i.ReceiverOwner,
			AccountEpoch: i.AccountEpoch, ReceiverRevision: i.Revision, DeploymentVersion: connection.version,
			UID: connection.uid, Sender: connection.sender, NativeGroup: group, Name: proof.group.Name,
			Team: proof.group.Team, Role: proof.role, ObservedAt: proof.at, IndividualMuted: proof.muted}, nil
	})
	if err != nil {
		return nativewire.RoleEvidence{}, err
	}
	evidence, ok := value.(nativewire.RoleEvidence)
	if !ok {
		return nativewire.RoleEvidence{}, ErrTextRoute
	}
	return evidence, nil
}
