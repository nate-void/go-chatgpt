package sensitive_filter

import (
	"chatgpt-web/pkg/config"
	"github.com/importcjj/sensitive"
	"log"
)

var filter *sensitive.Filter

func InitFilter() {
	cfg := config.GetConfig()
	if cfg.Sensitive.Dict != "" {
		filter = sensitive.New()

		if err := filter.LoadNetWordDict(cfg.Sensitive.Dict); err != nil {
			log.Println(err)
		}
	}
}

func GetFilter() *sensitive.Filter {
	return filter
}
