package config

func NewLogConf() *LogConf {
	return &LogConf{}
}

type LogConf struct {
	// Level: Define the log level, one of TRACE,DEBUG,WARN,ERROR,INFO
	Level string `json:"level" env:"LOG_LEVEL" envDefault:"TRACE"`
	// Colours: If enabled sets color to log labels
	Colours bool `json:"colours" env:"LOG_COLOR" envDefault:"true"`
	// FilePathEnabled: If enabled appends filepath to the end of the logs
	FilePathEnabled bool `json:"file_path_enabled" env:"LOG_FILE_PATH" envDefault:"true"`
}
