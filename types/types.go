package types

type DcosLog struct {
	Fields struct {
		AGENTID          string `json:"AGENT_ID"`
		CONTAINERID      string `json:"CONTAINER_ID"`
		DCOSSPACE        string `json:"DCOS_SPACE"`
		EXECUTORID       string `json:"EXECUTOR_ID"`
		FRAMEWORKID      string `json:"FRAMEWORK_ID"`
		MESSAGE          string `json:"MESSAGE"`
		STREAM           string `json:"STREAM"`
		SYSLOGIDENTIFIER string `json:"SYSLOG_IDENTIFIER"`
		SYSLOGTIMESTAMP  string `json:"@timestamp"`
	} `json:"fields"`
	Cursor             string `json:"cursor"`
	MonotonicTimestamp int64  `json:"monotonic_timestamp"`
	RealtimeTimestamp  int64  `json:"realtime_timestamp"`
}
