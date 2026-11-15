package gateway

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/nativeauth"
)

func (m *ReceiverManager) openMobileOnboarding(ctx context.Context, scope OnboardingNativeScope) (onboardingOutboundClient, SettingSnapshot, error) {
	if !scope.valid() || scope.Platform != "wsl" || m.MobileIDs == nil {
		return nil, SettingSnapshot{}, &nativeauth.Error{Code: "native_mobile_search_required"}
	}
	// Reuse the normal sealed desktop identity validation; this client never
	// performs a request and never becomes the mobile session.
	desktop, _, base, err := m.openSettingsClient(ctx, scope.AccountID, defaultSettingsClient)
	base.Platform = "wsl"
	if err != nil {
		return nil, base, err
	}
	desktop.Close()
	if !scope.matches(base) {
		return nil, base, ErrMemberScope
	}
	var mobile, deployment []byte
	err = m.DB.QueryRow(ctx, `SELECT m.credential_sealed,m.version,d.sealed,d.version
		FROM native_mobile_sessions m JOIN native_mobile_deployment d
		ON d.singleton AND d.version=m.deployment_version
		WHERE m.account_id=$1 AND m.native_uid=$2 AND m.account_epoch=$3
		AND m.state='authenticated' AND m.credential_sealed IS NOT NULL`,
		scope.AccountID, base.SelfID, base.AccountEpoch).Scan(
		&mobile, &base.MobileSessionVersion, &deployment, &base.MobileDeploymentVersion)
	if err == pgx.ErrNoRows {
		return nil, base, &nativeauth.Error{Code: "native_mobile_search_required"}
	}
	if err != nil {
		return nil, base, err
	}
	plain, err := m.Vault.Open("native-mobile-deployment:v1", deployment)
	if err != nil {
		return nil, base, &nativeauth.Error{Code: "native_mobile_configuration_invalid"}
	}
	d, err := nativeauth.ParseMobileDeployment(plain)
	clear(plain)
	if err != nil {
		return nil, base, &nativeauth.Error{Code: "native_mobile_configuration_invalid"}
	}
	plain, err = m.Vault.Open("native-mobile-session:"+scope.AccountID, mobile)
	if err != nil {
		return nil, base, &nativeauth.Error{Code: "native_mobile_search_required"}
	}
	var saved nativeauth.Session
	err = json.Unmarshal(plain, &saved)
	clear(plain)
	if err != nil || strconv.FormatUint(saved.UID, 10) != base.SelfID {
		clear(saved.MAC)
		return nil, base, &nativeauth.Error{Code: "native_mobile_search_required"}
	}
	client, err := nativeauth.ResumeMobile(d, saved, m.MobileIDs(base.MobileDeploymentVersion),
		m.mobileRequestCounts(scope, base))
	clear(saved.MAC)
	if err != nil {
		return nil, base, err
	}
	base.CaptchaID, base.SampledAt = d.CaptchaID, time.Now().UTC()
	if err = m.checkOnboardingAccount(ctx, scope, base); err != nil {
		client.Close()
		return nil, base, err
	}
	return wslOnboardingOutboundClient{nativeOnboardingOutboundClient: client}, base, nil
}

func (m *ReceiverManager) mobileRequestCounts(scope OnboardingNativeScope, base SettingSnapshot) nativeauth.RequestCountSource {
	return func(work context.Context) (uint64, error) {
		var next int64
		err := m.DB.QueryRow(work, `UPDATE native_mobile_sessions m SET request_count=request_count+1
			FROM accounts a,native_mobile_deployment d
			WHERE a.id=m.account_id AND m.account_id=$1 AND m.native_uid=$2
			AND a.native_uid=m.native_uid AND a.auth_state='authenticated'
			AND a.version=$6
			AND a.epoch=m.account_epoch AND m.account_epoch=$3
			AND m.version=$4 AND d.singleton AND d.version=$5 AND m.deployment_version=d.version
			AND m.state='authenticated' AND m.credential_sealed IS NOT NULL
			AND m.request_count<4294967295 RETURNING m.request_count-1`,
			scope.AccountID, base.SelfID, base.AccountEpoch,
			base.MobileSessionVersion, base.MobileDeploymentVersion, base.AccountVersion).Scan(&next)
		if err != nil {
			return 0, &nativeauth.Error{Code: "native_mobile_search_changed"}
		}
		return uint64(next), nil
	}
}

func (m *ReceiverManager) checkMobileOnboarding(ctx context.Context, scope OnboardingNativeScope, base SettingSnapshot) error {
	if base.MobileDeploymentVersion == 0 && base.MobileSessionVersion == 0 {
		return nil
	}
	if scope.Platform != "wsl" || base.MobileDeploymentVersion < 1 || base.MobileSessionVersion < 1 {
		return ErrMemberScope
	}
	var current bool
	err := m.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM native_mobile_sessions m
		JOIN native_mobile_deployment d ON d.version=m.deployment_version AND d.singleton
		WHERE m.account_id=$1 AND m.account_epoch=$2 AND m.native_uid=$3
		AND m.version=$4 AND d.version=$5 AND m.state='authenticated'
		AND m.credential_sealed IS NOT NULL)`, scope.AccountID, scope.AccountEpoch, base.SelfID,
		base.MobileSessionVersion, base.MobileDeploymentVersion).Scan(&current)
	if err != nil {
		return err
	}
	if !current {
		return &nativeauth.Error{Code: "native_mobile_search_changed"}
	}
	return nil
}
