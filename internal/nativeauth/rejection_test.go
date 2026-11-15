package nativeauth

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUnspecifiedRejectionPreservesOperation(t *testing.T) {
	for _, test := range []struct{ route, code string }{
		{"/v1/user/login", "native_login_rejected_unspecified"},
		{"/v1/user/register", "native_signup_rejected_unspecified"},
		{"/v1/verify/sms-anon", "native_sms_rejected_unspecified"},
		{"/v1/user/get-change-device-verify", "native_sms_rejected_unspecified"},
		{"/v1/settings/get-sys-cfg", "native_business_rejected"},
	} {
		if got := unspecifiedRejection(test.route); got != test.code {
			t.Errorf("%s: got %s, want %s", test.route, got, test.code)
		}
	}
}

func TestKnownRejectionDoesNotExposeProviderText(t *testing.T) {
	for _, test := range []struct{ route, message, code string }{
		{"/v1/user/login", "密码错误", "native_invalid_credentials"},
		{"/v1/user/login", "账号或密码错误，请重新输入", "native_invalid_credentials"},
		{"/v1/user/login", "账号或密码错误, 请重新输入", "native_invalid_credentials"},
		{"/v1/user/register", "账号或密码错误，请重新输入", ""},
		{"/v1/user/login", "账号或密码错误，请重新输入 synthetic-secret", ""},
		{"/v1/user/register", "密码错误", ""},
		{"/v1/user/register", "验证码错误", "native_verification_rejected"},
		{"/v1/verify/sms-anon", "验证码已过期", "native_verification_expired"},
		{"/v1/user/register", "短信验证码错误", "native_invalid_sms"},
		{"/v1/user/register", "该手机号已注册", "native_signup_phone_registered"},
		{"/v1/settings/get-sys-cfg", "该手机号已注册", ""},
		{"/v1/user/register", "unexpected synthetic password and token", ""},
		{"/v1/user/register", "<script>synthetic-only</script>", ""},
	} {
		if got := knownRejection(test.route, test.message); got != test.code {
			t.Errorf("%s: got %s, want %s", test.route, got, test.code)
		}
	}
}

func TestAuthBusinessCodesAreRouteAndPurposeScoped(t *testing.T) {
	signup := map[string]any{"ty": "VERIFY_FOR_REGISTER"}
	for _, test := range []struct {
		route, purpose string
		number         int
		want           string
	}{
		{"/v1/verify/sms-anon", "VERIFY_FOR_REGISTER", 1037, "native_signup_phone_registered"},
		{"/v1/verify/sms-anon", "VERIFY_FOR_LOGIN", 1037, "native_sms_rejected_unspecified"},
		{"/v1/verify/sms-anon", "VERIFY_FOR_RESET_PHONE", 1037, "native_sms_rejected_unspecified"},
		{"/v1/user/login", "", 1037, "native_login_rejected_unspecified"},
		{"/v1/user/register", "", 1037, "native_signup_phone_registered"},
		{"/v1/user/register", "", 1039, "native_signup_rejected_unspecified"},
		{"/v1/user/login", "", 1039, "native_phone_not_found"},
		{"/v1/verify/sms-anon", "VERIFY_FOR_REGISTER", 1000, "native_request_parameters_rejected"},
		{"/v1/verify/sms-anon", "VERIFY_FOR_REGISTER", 1002, "native_request_parameters_missing"},
		{"/v1/verify/sms-anon", "VERIFY_FOR_REGISTER", 1036, "native_verification_rejected"},
		{"/v1/verify/sms-anon", "VERIFY_FOR_REGISTER", 1038, "native_sms_rate_limited"},
		{"/v1/verify/sms-anon", "VERIFY_FOR_REGISTER", 1040, "native_captcha_rejected"},
		{"/v1/verify/sms-anon", "VERIFY_FOR_REGISTER", 1043, "native_rate_limited"},
		{"/v1/verify/sms-anon", "VERIFY_FOR_REGISTER", 1044, "native_verification_expired"},
		{"/v1/verify/sms-anon", "VERIFY_FOR_REGISTER", 1200, "native_client_version_rejected"},
		{"/v1/verify/sms-anon", "VERIFY_FOR_REGISTER", 1201, "native_ip_restricted"},
		{"/v1/settings/get-sys-cfg", "", 1038, "native_business_rejected"},
		{"/v1/group/set-group-mute", "", 1201, "native_business_rejected"},
	} {
		code, diagnostic := authBusinessRejection(test.route, map[string]any{"ty": test.purpose}, test.number, "")
		if code != test.want || strings.HasSuffix(code, "_unspecified") && diagnostic != "provider_reason_empty" {
			t.Fatalf("route %s purpose %s code %d: %s/%s", test.route, test.purpose, test.number, code, diagnostic)
		}
	}
	code, diagnostic := authBusinessRejection("/v1/verify/sms-anon", signup, 1001, "")
	if code != "native_sms_rejected_unspecified" || diagnostic != "provider_reason_empty" {
		t.Fatal("generic business 1001 acquired a transport or phone-range meaning")
	}
}

func TestSafeReasonClassificationNeverRetainsRawContent(t *testing.T) {
	for _, test := range []struct{ message, code, diagnostic string }{
		{"", "native_sms_rejected_unspecified", "provider_reason_empty"},
		{" \n\t ", "native_sms_rejected_unspecified", "provider_reason_empty"},
		{"该手机号已注册", "native_signup_phone_registered", "provider_reason_classified"},
		{" 短信发送过于频繁！ ", "native_sms_rate_limited", "provider_reason_classified"},
		{"请求过于频繁", "native_rate_limited", "provider_reason_classified"},
		{"安全验证失败", "native_captcha_rejected", "provider_reason_classified"},
		{"该号段不支持注册", "native_phone_range_restricted", "provider_reason_classified"},
		{"该手机号不支持注册", "native_phone_registration_restricted", "provider_reason_classified"},
		{"该号段不支持注册吗", "native_sms_rejected_unspecified", "provider_reason_unrecognized"},
		{"该号段不支持注册 13800000000", "native_sms_rejected_unspecified", "provider_reason_unrecognized"},
		{"密码=synthetic-secret 验证码=123456 token=synthetic-ticket", "native_sms_rejected_unspecified", "provider_reason_unrecognized"},
		{"<script>该号段不支持注册</script>", "native_sms_rejected_unspecified", "provider_reason_unrecognized"},
		{strings.Repeat(" ", 513), "native_sms_rejected_unspecified", "provider_reason_unrecognized"},
		{string([]byte{0xff, 0xfe}), "native_sms_rejected_unspecified", "provider_reason_unrecognized"},
	} {
		code, diagnostic := authBusinessRejection("/v1/verify/sms-anon", map[string]any{"ty": "VERIFY_FOR_REGISTER"}, 1001, test.message)
		if code != test.code || diagnostic != test.diagnostic {
			t.Fatalf("unexpected safe result %s/%s", code, diagnostic)
		}
		native := &Error{Code: code, Number: 1001, Diagnostic: diagnostic, Submitted: true}
		raw, _ := json.Marshal(native)
		for _, secret := range []string{"13800000000", "123456", "synthetic-secret", "synthetic-ticket", "<script>"} {
			if strings.Contains(string(raw), secret) || strings.Contains(native.Error(), secret) {
				t.Fatal("provider content escaped through error metadata")
			}
		}
	}
	for _, purpose := range []string{"VERIFY_FOR_LOGIN", "VERIFY_FOR_RESET_PHONE", ""} {
		code, _ := authBusinessRejection("/v1/verify/sms-anon", map[string]any{"ty": purpose}, 1001, "该号段不支持注册")
		if code != "native_sms_rejected_unspecified" {
			t.Fatal("registration restriction escaped SMS purpose")
		}
	}
}

func TestStructuredReasonPreservesCodeWithoutRetainingContent(t *testing.T) {
	for _, test := range []struct {
		raw        string
		diagnostic string
	}{
		{"", "provider_reason_empty"},
		{"null", "provider_reason_empty"},
		{`""`, "provider_reason_empty"},
		{`{"phone":"13800000000","token":"synthetic-secret"}`, "provider_reason_nontext"},
		{`["123456"]`, "provider_reason_nontext"},
		{`123456`, "provider_reason_nontext"},
		{`false`, "provider_reason_nontext"},
		{`"` + strings.Repeat("a", 513) + `"`, "provider_reason_oversized"},
		{`"` + strings.Repeat(`\u0061`, 513) + `"`, "provider_reason_oversized"},
		{`"\u8be5\u53f7\u6bb5\u4e0d\u652f\u6301\u6ce8\u518c"`, "provider_reason_classified"},
	} {
		code, diagnostic := authBusinessResponseRejection("/v1/verify/sms-anon",
			map[string]any{"ty": "VERIFY_FOR_REGISTER"}, 1001, json.RawMessage(test.raw))
		want := "native_sms_rejected_unspecified"
		if test.diagnostic == "provider_reason_classified" {
			want = "native_phone_range_restricted"
		}
		if code != want || diagnostic != test.diagnostic || safeAuthDiagnostic(diagnostic) != diagnostic {
			t.Fatalf("unexpected structured reason result %s/%s", code, diagnostic)
		}
		code, diagnostic = authBusinessResponseRejection("/v1/verify/sms-anon",
			map[string]any{"ty": "VERIFY_FOR_REGISTER"}, 1038, json.RawMessage(test.raw))
		if code != "native_sms_rate_limited" || diagnostic != "provider_code_classified" {
			t.Fatal("reason shape erased a verified business code")
		}
		code, diagnostic = authBusinessResponseRejection("/v1/group/set-group-mute", nil, 1001, json.RawMessage(test.raw))
		if code != "native_business_rejected" || diagnostic != "" {
			t.Fatal("authentication reason classification escaped its routes")
		}
	}
}
