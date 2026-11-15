package nativeauth

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Versioned, safe diagnostic enums allow a manual attempt to identify the
// classifier used without retaining the upstream response.
const AuthDiagnosticVersion = 3

// Generic provider codes do not identify a cause. Keep the operation, but
// never expose arbitrary provider text, which may contain credentials or PII.
func unspecifiedRejection(route string) string {
	switch route {
	case "/v1/user/login":
		return "native_login_rejected_unspecified"
	case "/v1/user/register":
		return "native_signup_rejected_unspecified"
	case "/v1/verify/sms-anon", "/v1/user/get-change-device-verify":
		return "native_sms_rejected_unspecified"
	default:
		return "native_business_rejected"
	}
}

// Interpret exact, bounded vocabulary only. Provider error text is never
// returned, logged or persisted, even when the numeric code is generic.
func knownRejection(route, message string) string {
	switch route {
	case "/v1/user/login", "/v1/user/register", "/v1/verify/sms-anon", "/v1/user/get-change-device-verify":
	default:
		return ""
	}
	switch message {
	case "账号或密码错误", "密码错误", "账号或密码错误，请重新输入", "账号或密码错误, 请重新输入":
		if route == "/v1/user/login" {
			return "native_invalid_credentials"
		}
	case "短信验证码错误":
		return "native_invalid_sms"
	case "验证码错误":
		return "native_verification_rejected"
	case "验证码已过期":
		return "native_verification_expired"
	case "手机号已注册", "该手机号已注册", "手机号已被注册":
		if route == "/v1/user/register" {
			return "native_signup_phone_registered"
		}
	case "短信发送频繁", "短信发送过于频繁", "短信验证码发送太过频繁", "短信发送频繁，请稍后再试",
		"短信发送过于频繁，请稍后再试":
		return "native_sms_rate_limited"
	case "请求过于频繁", "请求频繁，请稍后再试":
		return "native_rate_limited"
	case "人机验证失败", "安全验证失败", "安全验证未通过":
		return "native_captcha_rejected"
	case "该号段不支持注册", "该号段暂不支持注册", "该号段不允许注册", "此号段不支持注册",
		"暂不支持该号段注册", "手机号段不支持注册", "手机号段不允许注册":
		if route == "/v1/user/register" {
			return "native_phone_range_restricted"
		}
	case "该手机号不支持注册", "该手机号暂不支持注册", "该手机号不允许注册", "此手机号不支持注册":
		if route == "/v1/user/register" {
			return "native_phone_registration_restricted"
		}
	}
	return ""
}

// The official shared API schema defines these business codes. Restrict them
// to authentication routes and SMS purpose, never desktop transport status.
func authBusinessRejection(route string, params map[string]any, number int, message string) (string, string) {
	switch route {
	case "/v1/user/login", "/v1/user/register", "/v1/verify/sms-anon", "/v1/user/get-change-device-verify":
	default:
		return unspecifiedRejection(route), ""
	}
	signup := route == "/v1/user/register" ||
		route == "/v1/verify/sms-anon" && params["ty"] == "VERIFY_FOR_REGISTER"
	code := ""
	switch number {
	case 1000:
		code = "native_request_parameters_rejected"
	case 1002:
		code = "native_request_parameters_missing"
	case 1030, 1047:
		if route == "/v1/user/login" {
			code = "native_invalid_credentials"
		}
	case 1036:
		code = "native_verification_rejected"
	case 1037:
		if signup {
			code = "native_signup_phone_registered"
		}
	case 1038:
		code = "native_sms_rate_limited"
	case 1039:
		if !signup {
			code = "native_phone_not_found"
		}
	case 1040:
		code = "native_captcha_rejected"
	case 1043:
		code = "native_rate_limited"
	case 1044:
		code = "native_verification_expired"
	case 1200:
		code = "native_client_version_rejected"
	case 1201:
		code = "native_ip_restricted"
	}
	if code != "" {
		return code, "provider_code_classified"
	}
	// Bound inspection before normalization. Only enum results leave this
	// function; unrecognized text, identifiers and tokens are never retained.
	if len(message) <= 512 {
		message = strings.TrimSpace(message)
		if message == "" {
			return unspecifiedRejection(route), "provider_reason_empty"
		}
		knownRoute := route
		if signup {
			knownRoute = "/v1/user/register"
		}
		if known := knownRejection(knownRoute, strings.TrimRight(message, "。！!")); known != "" {
			return known, "provider_reason_classified"
		}
	}
	return unspecifiedRejection(route), "provider_reason_unrecognized"
}

func authBusinessResponseRejection(route string, params map[string]any, number int, raw json.RawMessage) (string, string) {
	code, diagnostic := authBusinessRejection(route, params, number, "")
	if diagnostic != "provider_reason_empty" {
		return code, diagnostic
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return code, "provider_reason_empty"
	}
	// A JSON string can encode each ASCII byte with six bytes (\uXXXX).
	if len(raw) > 6*512+2 {
		return code, "provider_reason_oversized"
	}
	var reason string
	if json.Unmarshal(raw, &reason) != nil {
		return code, "provider_reason_nontext"
	}
	if len(reason) > 512 {
		return code, "provider_reason_oversized"
	}
	return authBusinessRejection(route, params, number, reason)
}

func safeAuthDiagnostic(value string) string {
	switch value {
	case "network_timeout", "connection_closed", "network_unavailable",
		"login_identity_invalid", "login_account_mismatch", "login_phone_invalid",
		"login_phone_mismatch", "login_security_flags_invalid",
		"provider_code_classified", "provider_reason_classified",
		"provider_reason_empty", "provider_reason_unrecognized",
		"provider_reason_nontext", "provider_reason_oversized":
		return value
	default:
		return ""
	}
}
