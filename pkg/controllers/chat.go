package controllers

import (
	"chatgpt-web/pkg/data"
	"chatgpt-web/pkg/db/mysql"
	sensitiveFilter "chatgpt-web/pkg/sensitive-filter"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"chatgpt-web/pkg/tokenizer"
	"chatgpt-web/pkg/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/karlseguin/ccache/v3"
	"github.com/sashabaranov/go-openai"
	"k8s.io/klog/v2"
)

const (
	ChatPrimedTokens = 2
)

type ChatService struct {
	client         *openai.Client
	store          *ccache.Cache[ChatMessage]
	params         ChatCompletionParams
	account        *AccountService
	chatRecordData data.IChatRecordsData
}

type ChatCompletionParams struct {
	Model                 string        `json:"model"`
	MaxTokens             int           `json:"max_tokens,omitempty"`
	Temperature           float32       `json:"temperature,omitempty"`
	PresencePenalty       float32       `json:"presence_penalty,omitempty"`
	FrequencyPenalty      float32       `json:"frequency_penalty,omitempty"`
	ChatSessionTTL        time.Duration `json:"chat_session_ttl"`
	ChatMinResponseTokens int           `json:"chat_min_response_tokens"`
}

type ChatMessageRequest struct {
	Prompt  string                    `json:"prompt"`
	Options ChatMessageRequestOptions `json:"options"`
}

type ChatMessageRequestOptions struct {
	Name            string `json:"name"`
	ParentMessageId string `json:"parentMessageId"`
}

type ChatMessage struct {
	ID              string                              `json:"id"`
	Text            string                              `json:"text"`
	Role            string                              `json:"role"`
	Name            string                              `json:"name"`
	Delta           string                              `json:"delta"`
	Detail          openai.ChatCompletionStreamResponse `json:"detail"`
	TokenCount      int                                 `json:"tokenCount"`
	ParentMessageId string                              `json:"parentMessageId"`
}

func NewChatService(apiKey string, baseURL string, socksProxy string, params ChatCompletionParams, account *AccountService) (*ChatService, error) {
	config := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		config.BaseURL = baseURL
	}
	klog.Infof("use openai base url: %s", config.BaseURL)
	if socksProxy != "" {
		proxyUrl, err := url.Parse(socksProxy) //socks5://user:password@127.0.0.1:1080
		if err != nil {
			return nil, err
		}
		config.HTTPClient = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(proxyUrl),
			},
		}
		klog.Infof("use sock proxy: %s", proxyUrl)
	}
	db := mysql.GetDB()
	chatRecordsData := data.NewChatRecordsData(db)

	chat := ChatService{
		client:         openai.NewClientWithConfig(config),
		params:         params,
		store:          ccache.New(ccache.Configure[ChatMessage]()),
		account:        account,
		chatRecordData: chatRecordsData,
	}
	return &chat, nil
}

func (chat *ChatService) ChatProcess(ctx *gin.Context) {
	payload := ChatMessageRequest{}
	if err := ctx.BindJSON(&payload); err != nil {
		klog.Error(err)
		ctx.JSON(200, gin.H{
			"status":  "Fail",
			"message": fmt.Sprintf("%v", err),
			"data":    nil,
		})
		return
	}
	username := ctx.GetString("username")

	messageID := uuid.New().String()

	message := ChatMessage{
		ID:              messageID,
		Role:            openai.ChatMessageRoleUser,
		Text:            payload.Prompt,
		ParentMessageId: payload.Options.ParentMessageId,
	}

	result := ChatMessage{
		ID:              uuid.New().String(),
		Role:            openai.ChatMessageRoleAssistant,
		Text:            "",
		ParentMessageId: messageID,
	}
	//过滤敏感词
	chatPass := false
	filter := sensitiveFilter.GetFilter()
	if filter != nil {
		ok, _ := filter.Validate(message.Text)
		if ok {
			chatPass = true
		}
	}

	if !chatPass {
		result.Text = "触及到屏蔽词，不予回答"
		cr := &data.ChatRecord{
			UserMassage: message.Text,
			AIMessage:   result.Text,
			CreateAt:    time.Now().Unix(),
		}

		err := chat.chatRecordData.AddSensitive(cr)
		if err != nil {
			fmt.Println(err)
		}

		ctx.Header("Content-type", "application/octet-stream")
		bytes, _ := json.Marshal(result)
		_, err = ctx.Writer.Write(bytes)
		if err != nil {
			log.Println(err)
		}
		ctx.Writer.Flush()
		return
	}

	messages, numTokens, tokenCount, err := chat.buildMessage(payload)
	if err != nil {
		ctx.JSON(200, gin.H{
			"status":  "Fail",
			"message": fmt.Sprintf("%v", err),
			"data":    nil,
		})
		return
	}

	message.TokenCount = tokenCount
	chat.store.Set(messageID, message, chat.params.ChatSessionTTL)

	klog.Infof("use %s model, send message %d tokens, set completion %d max tokens", chat.params.Model, numTokens, chat.params.MaxTokens-numTokens)

	stream, err := chat.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model:            chat.params.Model,
		Messages:         messages,
		MaxTokens:        chat.params.MaxTokens - numTokens,
		Temperature:      chat.params.Temperature,
		PresencePenalty:  chat.params.PresencePenalty,
		FrequencyPenalty: chat.params.FrequencyPenalty,
		TopP:             1,
		Stream:           true,
	})
	if err != nil {
		klog.Error(err)
		ctx.JSON(200, gin.H{
			"status":  "Fail",
			"message": fmt.Sprintf("%v", err),
			"data":    nil,
		})
		return
	}
	defer stream.Close()

	resp := stream.GetResponse()
	if resp.StatusCode != 200 {
		bts, _ := io.ReadAll(resp.Body)
		ctx.JSON(200, gin.H{
			"status":  "Fail",
			"message": fmt.Sprintf("%v", string(bts)),
			"data":    nil,
		})
		return
	}

	firstChunk := true
	defer func() {
		if result.Text != "" {
			go func() {
				tokenCount, err := tokenizer.GetTokenCount(openai.ChatCompletionMessage{
					Role:    result.Role,
					Content: result.Text,
					Name:    result.Name,
				}, chat.params.Model)
				if err != nil {
					klog.Error(err)
				}
				result.TokenCount = tokenCount
				chat.store.Set(result.ID, result, chat.params.ChatSessionTTL)
				if err := chat.account.IncUsage(username, int64(tokenCount+numTokens-ChatPrimedTokens)); err != nil {
					fmt.Println(err)
				}

				cr := &data.ChatRecord{
					UserMassage: message.Text,
					AIMessage:   result.Text,
					CreateAt:    time.Now().Unix(),
				}
				if err := chat.chatRecordData.Add(cr); err != nil {
					fmt.Println(err)
				}

			}()
		}
	}()
	ctx.Header("Content-type", "application/octet-stream")
	for {
		rsp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return
		}

		if err != nil {
			klog.Error(err)
			ctx.JSON(200, gin.H{
				"status":  "Fail",
				"message": fmt.Sprintf("OpenAI Event Error %v", err),
				"data":    nil,
			})
			return
		}

		if rsp.ID != "" {
			result.ID = rsp.ID
		}

		if len(rsp.Choices) > 0 {
			content := rsp.Choices[0].Delta.Content
			result.Delta = content
			if len(content) > 0 {
				result.Text += content
			}
			result.Detail = rsp
		}

		bts, err := json.Marshal(result)
		if err != nil {
			klog.Error(err)
			ctx.JSON(200, gin.H{
				"status":  "Fail",
				"message": fmt.Sprintf("OpenAI Event Marshal Error %v", err),
				"data":    nil,
			})
			return
		}

		if !firstChunk {
			if _, err := ctx.Writer.Write([]byte("\n")); err != nil {
				fmt.Println(err)
			}
		} else {
			firstChunk = false
		}

		if _, err := ctx.Writer.Write(bts); err != nil {
			klog.Error(err)
			return
		}

		ctx.Writer.Flush()
	}
}

func (chat *ChatService) MessageProcess(ctx *gin.Context) {
	payload := ChatMessageRequest{}
	if err := ctx.BindJSON(&payload); err != nil {
		klog.Error(err)
		ctx.JSON(200, gin.H{
			"status":  "Fail",
			"message": fmt.Sprintf("%v", err),
			"data":    nil,
		})
		return
	}
	username := ctx.GetString("username")

	messageID := uuid.New().String()

	message := ChatMessage{
		ID:              messageID,
		Role:            openai.ChatMessageRoleUser,
		Text:            payload.Prompt,
		ParentMessageId: payload.Options.ParentMessageId,
	}

	result := ChatMessage{
		ID:              uuid.New().String(),
		Role:            openai.ChatMessageRoleAssistant,
		Text:            "",
		ParentMessageId: messageID,
	}

	messages, numTokens, tokenCount, err := chat.buildMessage(payload)
	if err != nil {
		ctx.JSON(200, gin.H{
			"status":  "Fail",
			"message": fmt.Sprintf("%v", err),
			"data":    nil,
		})
		return
	}

	message.TokenCount = tokenCount
	chat.store.Set(messageID, message, chat.params.ChatSessionTTL)

	klog.Infof("use %s model, send message %d tokens, set completion %d max tokens", chat.params.Model, numTokens, chat.params.MaxTokens-numTokens)

	resp, err := chat.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:            chat.params.Model,
		Messages:         messages,
		MaxTokens:        chat.params.MaxTokens - numTokens,
		Temperature:      chat.params.Temperature,
		PresencePenalty:  chat.params.PresencePenalty,
		FrequencyPenalty: chat.params.FrequencyPenalty,
		TopP:             1,
		Stream:           false,
	})
	if err != nil {
		klog.Error(err)
		ctx.JSON(200, gin.H{
			"status":  "Fail",
			"message": fmt.Sprintf("%v", err),
			"data":    nil,
		})
		return
	}

	if len(resp.Choices) > 0 {
		content := resp.Choices[0].Message.Content
		result.Delta = content
		result.Text += content
		result.Detail = openai.ChatCompletionStreamResponse{
			ID:      resp.ID,
			Object:  resp.Object,
			Created: resp.Created,
			Model:   resp.Model,
			Choices: []openai.ChatCompletionStreamChoice{
				// {
				// 	Index:        resp.Choices[0].Index,
				// 	FinishReason: resp.Choices[0].FinishReason,
				// 	Delta: openai.ChatCompletionStreamChoiceDelta{
				// 		Content: content,
				// 	},
				// },
			},
		}
	}

	if resp.ID != "" {
		result.ID = resp.ID
	}
	//issue why usage.completion_tokens less 8 than tiktoken?
	result.TokenCount = resp.Usage.TotalTokens + 8

	defer func() {
		if result.Text != "" {
			go func() {
				tokenCount, err := tokenizer.GetTokenCount(openai.ChatCompletionMessage{
					Role:    result.Role,
					Content: result.Text,
					Name:    result.Name,
				}, chat.params.Model)
				if err != nil {
					klog.Error(err)
				}
				result.TokenCount = tokenCount
				chat.store.Set(result.ID, result, chat.params.ChatSessionTTL)
				if err := chat.account.IncUsage(username, int64(tokenCount+numTokens-ChatPrimedTokens)); err != nil {
					fmt.Println(err)
				}
			}()
		}
	}()

	if _, err := json.Marshal(result); err != nil {
		klog.Error(err)
		ctx.JSON(200, gin.H{
			"status":  "Fail",
			"message": fmt.Sprintf("OpenAI Event Marshal Error %v", err),
			"data":    nil,
		})
		return
	}
	ctx.JSON(200, result)
}

func (chat *ChatService) buildMessage(payload ChatMessageRequest) ([]openai.ChatCompletionMessage, int, int, error) {
	parentMessageId := payload.Options.ParentMessageId
	var messages []openai.ChatCompletionMessage
	tokenCount := 0
	var err error
	if len(payload.Prompt) > 0 {
		chatMessage := openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: payload.Prompt,
			Name:    payload.Options.Name,
		}
		messages = append(messages, chatMessage)
		tokenCount, err = tokenizer.GetTokenCount(chatMessage, chat.params.Model)
		if err != nil {
			return nil, 0, 0, err
		}
		if tokenCount >= (chat.params.MaxTokens - chat.params.ChatMinResponseTokens) {
			return nil, 0, 0, fmt.Errorf("this model's maximum context length is %d tokens. you requested %d tokens in the messages", chat.params.MaxTokens, tokenCount)
		}
	}
	numTokens := tokenCount + ChatPrimedTokens
	for {
		if parentMessageId == "" {
			break
		}
		parentMessage, ok := chat.getMessageByID(parentMessageId)
		if !ok {
			break
		}
		parentCompleteIOMessage := openai.ChatCompletionMessage{
			Role:    parentMessage.Role,
			Content: parentMessage.Text,
			Name:    parentMessage.Name,
		}
		if (numTokens + parentMessage.TokenCount) >= (chat.params.MaxTokens - chat.params.ChatMinResponseTokens) {
			break
		}
		numTokens += parentMessage.TokenCount
		messages = append(messages, parentCompleteIOMessage)
		parentMessageId = parentMessage.ParentMessageId
	}
	utils.Reverse(messages)
	return messages, numTokens, tokenCount, nil
}

func (chat *ChatService) getMessageByID(id string) (ChatMessage, bool) {
	item := chat.store.Get(id)
	if item == nil {
		return ChatMessage{}, false
	}
	if item.Expired() {
		return ChatMessage{}, false
	}
	return item.Value(), true
}
