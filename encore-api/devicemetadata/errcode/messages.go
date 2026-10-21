package errcode

import (
	"net/http"
	"strings"
)

// messages maps error codes to i18n translations
var messages = map[string]map[string]string{
	"THING_MODEL_NOT_FOUND": {
		"zh-CN": "物模型未找到",
		"en-US": "Thing model not found",
	},
	"THING_MODEL_SNAPSHOT_NOT_FOUND": {
		"zh-CN": "物模型快照未找到",
		"en-US": "Thing model snapshot not found",
	},
	"THING_MODEL_IDENTIFIER_DUPLICATE": {
		"zh-CN": "标识符已存在",
		"en-US": "Identifier already exists",
	},
	"THING_MODEL_IDENTIFIER_INVALID": {
		"zh-CN": "无效的标识符",
		"en-US": "Invalid identifier",
	},
	"THING_MODEL_IDENTIFIER_RESERVED": {
		"zh-CN": "标识符被保留",
		"en-US": "Identifier is reserved",
	},
	"THING_MODEL_VALUE_TYPE_INVALID": {
		"zh-CN": "无效的数据类型",
		"en-US": "Invalid value type",
	},
	"THING_MODEL_ACCESS_INVALID": {
		"zh-CN": "无效的访问权限",
		"en-US": "Invalid access permission",
	},
	"THING_MODEL_VALUE_INVALID": {
		"zh-CN": "无效的值",
		"en-US": "Invalid value",
	},
	"THING_MODEL_RELATED_NOT_FOUND": {
		"zh-CN": "关联的资源未找到",
		"en-US": "Related resource not found",
	},
	"THING_MODEL_BAD_ACCESS": {
		"zh-CN": "访问被拒绝",
		"en-US": "Access denied",
	},
	"THING_MODEL_IMMUTABLE": {
		"zh-CN": "资源不可修改",
		"en-US": "Resource is immutable",
	},
	"THING_MODEL_BAD_TRANSITION": {
		"zh-CN": "状态转换无效",
		"en-US": "Invalid state transition",
	},
	"THING_MODEL_VERSION_CONFLICT": {
		"zh-CN": "版本冲突",
		"en-US": "Version conflict",
	},
	"THING_MODEL_META_KEY_UNKNOWN": {
		"zh-CN": "未知的元数据键",
		"en-US": "Unknown meta key",
	},
	"THING_MODEL_META_VALUE_INVALID": {
		"zh-CN": "无效的元数据值",
		"en-US": "Invalid meta value",
	},
	"THING_MODEL_HAS_REFERENCES": {
		"zh-CN": "资源仍有被引用",
		"en-US": "Resource still has references",
	},
	"DEVICE_TEMPLATE_NOT_FOUND": {
		"zh-CN": "设备模板未找到",
		"en-US": "Device template not found",
	},
	"DEVICE_TEMPLATE_ARCHIVED": {
		"zh-CN": "设备模板已存档",
		"en-US": "Device template is archived",
	},
	"PRODUCT_NOT_FOUND": {
		"zh-CN": "产品未找到",
		"en-US": "Product not found",
	},
	"PRODUCT_HAS_DEVICES": {
		"zh-CN": "产品仍有设备",
		"en-US": "Product still has devices",
	},
	"PRODUCT_TEMPLATE_MISMATCH": {
		"zh-CN": "产品与设备模板不匹配",
		"en-US": "Product template mismatch",
	},
	"TENANT_FORBIDDEN": {
		"zh-CN": "租户无权访问",
		"en-US": "Tenant access forbidden",
	},
	"UNKNOWN_PROTOCOL": {
		"zh-CN": "未知的协议",
		"en-US": "Unknown protocol",
	},
	"I18N_INVALID": {
		"zh-CN": "无效的国际化数据",
		"en-US": "Invalid i18n data",
	},
	"DEVICE_NOT_FOUND": {
		"zh-CN": "设备未找到",
		"en-US": "Device not found",
	},
}

// Message returns the localized message for the given error code.
// Falls back to zh-CN if the requested locale is not available.
func Message(code error, locale string) string {
	if code == nil {
		return ""
	}

	// Normalize locale
	if locale == "" {
		locale = "zh-CN"
	}

	codeStr := code.Error()
	locales, ok := messages[codeStr]
	if !ok {
		return codeStr
	}

	// Try exact match first
	if msg, ok := locales[locale]; ok {
		return msg
	}

	// Try language fallback (e.g., "en" for "en-US")
	if idx := strings.Index(locale, "-"); idx > 0 {
		lang := locale[:idx]
		for k, msg := range locales {
			if strings.HasPrefix(k, lang) {
				return msg
			}
		}
	}

	// Fall back to zh-CN
	if msg, ok := locales["zh-CN"]; ok {
		return msg
	}

	// Fall back to en-US
	if msg, ok := locales["en-US"]; ok {
		return msg
	}

	// Last resort: return code string
	return codeStr
}

// HTTPStatusCode maps error codes to HTTP status codes
func HTTPStatusCode(code error) int {
	if code == nil {
		return http.StatusInternalServerError
	}

	switch code {
	// 404 Not Found
	case ErrNotFound, ErrSnapshotNotFound, ErrTemplateNotFound, ErrProductNotFound, ErrDeviceNotFound:
		return http.StatusNotFound

	// 400 Bad Request
	case ErrIdentifierInvalid, ErrValueTypeInvalid, ErrAccessInvalid, ErrValueInvalid, ErrI18nInvalid:
		return http.StatusBadRequest

	// 409 Conflict
	case ErrIdentifierDuplicate, ErrVersionConflict:
		return http.StatusConflict

	// 412 Precondition Failed
	case ErrImmutable, ErrBadTransition:
		return http.StatusPreconditionFailed

	// 403 Forbidden
	case ErrTenantForbidden, ErrBadAccess:
		return http.StatusForbidden

	// 422 Unprocessable Entity
	case ErrRelatedNotFound, ErrProductHasDevices, ErrProductTemplateMismatch, ErrTemplateArchived, ErrHasReferences:
		return http.StatusUnprocessableEntity

	// 500 Internal Server Error
	default:
		return http.StatusInternalServerError
	}
}
