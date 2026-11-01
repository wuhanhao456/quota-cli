package render

import "regexp"

// The image and terminal output are in Chinese. JSON keeps the providers' English labels so
// that agents see a stable schema; these helpers translate them for display only.

var zhLabels = map[string]string{
	"Session (5h)":           "5 小时会话",
	"Session usage":          "5 小时会话",
	"Weekly":                 "每周",
	"Weekly usage":           "每周",
	"Weekly (all models)":    "每周（全部模型）",
	"Weekly (OAuth apps)":    "每周（OAuth 应用）",
	"Monthly":                "每月",
	"Monthly credits":        "每月额度",
	"Monthly included usage": "每月包含用量",
	"Extra usage (monthly)":  "额外用量（每月）",
	"Monthly credits left":   "本月剩余额度",
	"Purchased credits":      "购买的额度",
	"Free credits":           "赠送额度",
	"Credit balance":         "余额",
	"Credits":                "积分",
	"unlimited":              "无限",
}

var (
	weeklyModel = regexp.MustCompile(`^Weekly \((.+)\)$`)
	window      = regexp.MustCompile(`^Window \(([\d.]+)([hd])\)$`)
)

// zh translates a provider label. A label may carry a prefix such as a model name
// ("GPT-5-Codex-Spark Weekly"); the prefix is kept as is.
func zh(label string) string {
	if s, ok := zhExact(label); ok {
		return s
	}
	for i := range label {
		if label[i] != ' ' {
			continue
		}
		if s, ok := zhExact(label[i+1:]); ok {
			return label[:i] + " " + s
		}
	}
	return label
}

func zhExact(label string) (string, bool) {
	if s, ok := zhLabels[label]; ok {
		return s, true
	}
	if m := weeklyModel.FindStringSubmatch(label); m != nil {
		return "每周（" + m[1] + "）", true
	}
	if m := window.FindStringSubmatch(label); m != nil {
		if m[2] == "h" {
			return m[1] + " 小时窗口", true
		}
		return m[1] + " 天窗口", true
	}
	return "", false
}
