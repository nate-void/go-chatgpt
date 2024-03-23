package main

import (
	"chatgpt-web/pkg/config"
	"chatgpt-web/pkg/db/mysql"
	sensitive_filter "chatgpt-web/pkg/sensitive-filter"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"

	"chatgpt-web/pkg/controllers"
	"chatgpt-web/pkg/middlewares"
	"chatgpt-web/pkg/utils"

	"github.com/gin-gonic/gin"
	"k8s.io/klog/v2"
)

type ChatGPTWebServer struct {
	*config.Config
}

var Version = "0.0.0-dev"

func (r *ChatGPTWebServer) Run(ctx context.Context /*cmd *cobra.Command, args []string*/) error {
	if r.Chat.Version {
		return r.ShowVersion()
	}
	gin.SetMode(gin.ReleaseMode)
	if err := r.updateAssetsFiles(r.Chat.OpsLink); err != nil {
		return err
	}
	go r.startTokenizer(ctx)
	go r.httpServer(ctx)

	return nil
}

func (r *ChatGPTWebServer) httpServer(ctx context.Context) {
	accountService, err := controllers.NewAccountService(r.DB.DSN, r.Chat.BasicAuthUser, r.Chat.BasicAuthPassword)
	if err != nil {
		klog.Fatal(err)
	}
	chatService, err := controllers.NewChatService(r.Chat.OpenAIKey, r.Chat.OpenAIBaseURL, r.Chat.SocksProxy, controllers.ChatCompletionParams{
		Model:                 r.Chat.OpenAIModel,
		MaxTokens:             r.Chat.OpenAIMaxTokens,
		Temperature:           float32(r.Chat.OpenAITemperature) / 100.0,
		PresencePenalty:       float32(r.Chat.OpenAIPresencePenalty) / 100.0,
		FrequencyPenalty:      float32(r.Chat.OpenAIFrequencyPenalty) / 100.0,
		ChatSessionTTL:        time.Duration(r.Chat.ChatSessionTTL) * time.Minute,
		ChatMinResponseTokens: r.Chat.ChatMinResponseTokens,
	}, accountService)
	if err != nil {
		klog.Fatal(err)
	}

	addr := fmt.Sprintf("%s:%d", r.Http.Host, r.Http.Port)
	klog.Infof("ChatGPT Web Server on: %s", addr)
	server := &http.Server{
		Addr: addr,
	}
	entry, proxy := gin.New(), gin.New()
	entry.Use(gin.Logger())
	entry.Use(gin.Recovery())
	chat := entry.Group("/api")
	chat.POST("/chat-process", BasicAuth(accountService, r.Chat.OpsLink), middlewares.RateLimitMiddleware(1, 2), chatService.ChatProcess)
	chat.POST("/process", BasicAuth(accountService, r.Chat.OpsLink), middlewares.RateLimitMiddleware(1, 2), chatService.MessageProcess)
	chat.POST("/config", func(ctx *gin.Context) {
		ctx.JSON(200, gin.H{
			"status": "Success",
			"data": map[string]string{
				"apiModel":   "ChatGPTAPI",
				"socksProxy": r.Chat.SocksProxy,
			},
		})
	})
	chat.POST("/session", func(ctx *gin.Context) {
		ctx.JSON(200, gin.H{
			"status":  "Success",
			"message": "",
			"data": gin.H{
				"auth": false,
			},
		})
	})

	entry.POST("/accounts", OpsAuth(r.Chat.OpsKey), accountService.AccountProcess)
	entry.Any("/admin/*relativePath", gin.BasicAuth(gin.Accounts{"admin": r.Chat.OpsKey}), func(ctx *gin.Context) {
		if ctx.Request.URL.Path == "/admin/accounts" {
			accountService.AccountProcess(ctx)
		} else {
			http.FileServer(http.Dir(r.Frontend.AdminPath)).ServeHTTP(ctx.Writer, ctx.Request)
		}
	})
	if r.Chat.OpenAIProxy {
		klog.Info("enable proxy openai api server")
		upstreamURL, err := url.Parse(strings.TrimSuffix(r.Chat.OpenAIBaseURL, "/v1"))
		if err != nil {
			klog.Fatal(err)
		}
		upstream := httputil.NewSingleHostReverseProxy(upstreamURL)
		if r.Chat.SocksProxy != "" {
			proxyUrl, err := url.Parse(r.Chat.SocksProxy)
			if err != nil {
				klog.Fatal(err)
			}
			upstream.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxyUrl),
			}
		}
		apis := proxy.Group("/v1")
		apis.Any("/*relativePath", func(ctx *gin.Context) {
			ctx.Request.Host = upstreamURL.Host
			upstream.ServeHTTP(ctx.Writer, ctx.Request)
		})
		proxy.NoRoute(func(ctx *gin.Context) {
			http.FileServer(http.Dir(r.Frontend.Path)).ServeHTTP(ctx.Writer, ctx.Request)
		})
		entry.NoRoute(func(ctx *gin.Context) {
			proxy.ServeHTTP(ctx.Writer, ctx.Request)
		})
	} else {
		entry.NoRoute(func(ctx *gin.Context) {
			http.FileServer(http.Dir(r.Frontend.Path)).ServeHTTP(ctx.Writer, ctx.Request)
		})
	}

	server.Handler = entry
	go func(ctx context.Context) {
		<-ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown with error %v", err)
		}
	}(ctx)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server listen and serve error %v", err)
	}
}

func (r *ChatGPTWebServer) startTokenizer(ctx context.Context) {
	args := strings.Split(fmt.Sprintf("nuxt --port %d --module tokenizer.py --workers 2", r.Tokenizer.Port), " ")
	klog.Infof("Start Tokenizer with %v", args)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		klog.Error(err)
		//os.Exit(1)
	}
}

func (r *ChatGPTWebServer) updateAssetsFiles(link string) error {
	pairs := map[string]string{}
	old := `{avatar:"https://raw.githubusercontent.com/Chanzhaoyu/chatgpt-web/main/src/assets/avatar.jpg",name:"ChenZhaoYu",description:'Star on <a href="https://github.com/Chanzhaoyu/chatgpt-bot" class="text-blue-500" target="_blank" >Github</a>'}`
	new := fmt.Sprintf(`{avatar:"https://raw.githubusercontent.com/Chanzhaoyu/chatgpt-web/main/src/assets/avatar.jpg",name:"获取帮助输入/help",description:'<a href="%s" class="text-blue-500" target="_blank" >自助中心</a>'}`, link)
	pairs[old] = new
	old = `{}.VITE_GLOB_OPEN_LONG_REPLY`
	new = `{VITE_GLOB_OPEN_LONG_REPLY:"true"}.VITE_GLOB_OPEN_LONG_REPLY`
	pairs[old] = new
	old = `<link rel="manifest" href="/manifest.webmanifest"><script id="vite-plugin-pwa:register-sw" src="/registerSW.js"></script>`
	new = ``
	pairs[old] = new
	old = `[y(" 此项目开源于 "),e("a",{class:"text-blue-600 dark:text-blue-500",href:"https://github.com/Chanzhaoyu/chatgpt-web",target:"_blank"}," Github "),y(" ，免费且基于 MIT 协议，没有任何形式的付费行为！ ")]`
	new = `[y(" 此项目开源于 "),e("a",{class:"text-blue-600 dark:text-blue-500",href:"https://github.com/Arvintian/chatgpt-web",target:"_blank"}," Github ")]`
	pairs[old] = new
	return utils.ReplaceFiles(r.Frontend.Path, pairs)
}

func (r *ChatGPTWebServer) ShowVersion() error {
	fmt.Println(Version)
	return nil
}

func NewChatGPTWebServer() *ChatGPTWebServer {
	return &ChatGPTWebServer{
		config.GetConfig(),
	}
}

var cfg = flag.String("config", "config.yaml", "config path")

func main() {
	log.SetFlags(log.LstdFlags | log.Llongfile)
	flag.Parse()
	config.LoadConfig(*cfg)

	fmt.Println(config.GetConfig())

	//init sensitive
	sensitive_filter.InitFilter()
	//database
	mysql.InitDB()

	ctx := context.Background()
	app := NewChatGPTWebServer()
	err := app.Run(ctx)
	if err != nil {
		log.Fatalln(err)
	}
	ctx, stop := signal.NotifyContext(ctx, os.Kill, os.Interrupt)
	defer stop()
	<-ctx.Done()

}
