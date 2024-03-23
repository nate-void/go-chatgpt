package config

import (
	"fmt"
	"log"

	"github.com/spf13/viper"
)

type Config struct {
	Http struct {
		Host string
		Port int
	}
	Tokenizer struct {
		Host string
		Port int
	}
	Frontend struct {
		Path      string
		AdminPath string `yaml:"admin_path" mapstruct:"admin_path"`
	}
	Chat struct {
		BasicAuthUser          string `mapstruct:"auth-user"`
		BasicAuthPassword      string `mapstruct:"auth-password"`
		OpsKey                 string `mapstruct:"ops-key"`
		OpsLink                string `mapstruct:"ops-link"`
		SocksProxy             string `mapstruct:"socks-proxy"`
		ChatSessionTTL         int    `mapstruct:"chat-session-ttl"`
		ChatMinResponseTokens  int    `mapstruct:"chat-min-response-tokens"`
		OpenAIKey              string `mapstruct:"openai-key"`
		OpenAIBaseURL          string `mapstruct:"openai-base-url"`
		OpenAIModel            string `mapstruct:"openai-model"`
		OpenAIMaxTokens        int    `mapstruct:"openai-max-tokens"`
		OpenAITemperature      int    `mapstruct:"openai-temperature"`
		OpenAIPresencePenalty  int    `mapstruct:"openai-presence-penalty"`
		OpenAIFrequencyPenalty int    `mapstruct:"openai-frequency-penalty"`
		OpenAIProxy            bool   `mapstruct:"openai-proxy"`
		Version                bool   `mapstruct:"version"`
	}

	Sensitive struct {
		Dict string
	}

	DB struct {
		DSN string
	}
}

var cfg *Config

func LoadConfig(path string) *Config {

	if path == "" {
		log.Fatalln("please set --config")
	}
	if cfg == nil {
		viper.SetConfigType("yaml")
		viper.SetConfigName("config.yaml")
		viper.AddConfigPath("./configs")
		// // 以下 2 行，将 viper.Get(key) key 字符串中 '.' 和 '-' 替换为 '_'
		// replacer := strings.NewReplacer(".", "_")
		// viper.SetEnvKeyReplacer(replacer)
		if err := viper.ReadInConfig(); err != nil {
			log.Fatalln(err.Error())
		}
		cfg = &Config{}
		// if err := v.Unmarshal(cfg); err != nil {
		// 	log.Fatalln(err.Error())
		// }
	}

	cfg.Http.Host = viper.GetString("http.host")
	cfg.Http.Port = viper.GetInt("http.port")
	cfg.Tokenizer.Host = viper.GetString("tokenizer.host")
	cfg.Tokenizer.Port = viper.GetInt("tokenizer.port")
	cfg.Frontend.Path = viper.GetString("frontend.path")
	cfg.Frontend.AdminPath = viper.GetString("frontend.admin-path")
	cfg.Chat.BasicAuthUser = viper.GetString("chat.auth-user")
	cfg.Chat.BasicAuthPassword = viper.GetString("chat.auth-password")
	cfg.Chat.OpsKey = viper.GetString("chat.OpsKey")
	cfg.Chat.OpsLink = viper.GetString("chat.OpsLink")
	cfg.Chat.SocksProxy = viper.GetString("chat.socks-proxy")
	cfg.Chat.ChatSessionTTL = viper.GetInt("chat.chat-session-TTL")
	cfg.Chat.ChatMinResponseTokens = viper.GetInt("chat.chat-min-response-tokens")
	cfg.Chat.OpenAIKey = viper.GetString("chat.openai-key")
	cfg.Chat.OpenAIBaseURL = viper.GetString("chat.openai-base-url")
	cfg.Chat.OpenAIModel = viper.GetString("chat.openai-model")
	cfg.Chat.OpenAIMaxTokens = viper.GetInt("chat.openai-max-tokens")
	cfg.Chat.OpenAITemperature = viper.GetInt("chat.openai-temperature")
	cfg.Chat.OpenAIPresencePenalty = viper.GetInt("chat.openai-presence-penalty")
	cfg.Chat.OpenAIFrequencyPenalty = viper.GetInt("chat.openai-frequency-penalty")
	cfg.Chat.OpenAIProxy = viper.GetBool("chat.openai-proxy")
	cfg.Chat.Version = viper.GetBool("chat.show-version")

	cfg.Sensitive.Dict = viper.GetString("sensitive.dict")
	cfg.DB.DSN = viper.GetString("DB.DSN")

	fmt.Println("Using config file: ", viper.ConfigFileUsed())

	return cfg

}

func GetConfig() *Config {
	return cfg
}
