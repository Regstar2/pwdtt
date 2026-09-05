package backend

type ConnectParams struct {
	PeerAddr        string   `json:"peerAddr"`
	Password        string   `json:"password"`
	Hashes          []string `json:"hashes"`
	DeviceID        string   `json:"deviceId,omitempty"`
	Workers         int      `json:"workers,omitempty"`
	CaptchaMode     string   `json:"captchaMode,omitempty"`
	ObfsMode        string   `json:"obfsMode,omitempty"`
	Fingerprint     string   `json:"fingerprint,omitempty"`
	ProfileName     string   `json:"profileName,omitempty"`
	HashMode        string   `json:"hashMode,omitempty"`
	HashAutoCheck   bool     `json:"hashAutoCheck,omitempty"`
	HashAutoReplace bool     `json:"hashAutoReplace,omitempty"`
	OperationID     string   `json:"operationId,omitempty"`
}

type VKHashPolicy struct {
	Mode        string `json:"mode"`
	AutoCheck   bool   `json:"autoCheck"`
	AutoReplace bool   `json:"autoReplace"`
}

type VKHashCheck struct {
	Status    string `json:"status"`
	CheckedAt int64  `json:"checkedAt"`
	ErrorType string `json:"errorType,omitempty"`
	Message   string `json:"message,omitempty"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
}

type VKHashEntry struct {
	ID        string                 `json:"id"`
	Hash      string                 `json:"hash"`
	Source    string                 `json:"source"`
	InPool    bool                   `json:"inPool"`
	CreatedAt int64                  `json:"createdAt"`
	Checks    map[string]VKHashCheck `json:"checks,omitempty"`
	UsedBy    []string               `json:"usedBy,omitempty"`
}

type VKHashCheckResult struct {
	HashID      string `json:"hashId"`
	Hash        string `json:"hash"`
	ProfileName string `json:"profileName"`
	Status      string `json:"status"`
	CheckedAt   int64  `json:"checkedAt"`
	ErrorType   string `json:"errorType,omitempty"`
	Message     string `json:"message,omitempty"`
	LatencyMs   int64  `json:"latencyMs,omitempty"`
}

type ProfileData struct {
	PeerAddr   string        `json:"peer"`
	Password   string        `json:"password"`
	Hashes     []string      `json:"hashes"`
	Listen     string        `json:"listen"`
	TurnHost   string        `json:"turn"`
	TurnPort   string        `json:"port"`
	DeviceID   string        `json:"device_id"`
	HashPolicy *VKHashPolicy `json:"hash_policy,omitempty"`
}

type AppSettings struct {
	AutoStart     bool   `json:"autoStart"`
	ObfsMode      string `json:"obfsMode"`
	ObfsAccepted  bool   `json:"obfsAccepted"`
	DebugLogging bool   `json:"debugLogging,omitempty"`
}
