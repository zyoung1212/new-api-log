package relay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"one-api/common"
	"one-api/dto"
	relaycommon "one-api/relay/common"
	"one-api/relay/helper"
	"one-api/service"
	"one-api/setting/model_setting"
	"one-api/types"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func getAndValidateClaudeRequest(c *gin.Context) (textRequest *dto.ClaudeRequest, err error) {
	textRequest = &dto.ClaudeRequest{}
	err = c.ShouldBindJSON(textRequest)
	if err != nil {
		return nil, err
	}
	if textRequest.Messages == nil || len(textRequest.Messages) == 0 {
		return nil, errors.New("field messages is required")
	}
	if textRequest.Model == "" {
		return nil, errors.New("field model is required")
	}
	return textRequest, nil
}

func ClaudeHelper(c *gin.Context) (newAPIError *types.NewAPIError) {
	startTime := time.Now()

	relayInfo := relaycommon.GenRelayInfoClaude(c)

	// [CLAUDE] 请求开始日志
	common.LogInfo(c, fmt.Sprintf("[CLAUDE] Request started | User:%d | Channel:%d | Model:%s | IsStream:%v", 
		relayInfo.UserId, relayInfo.ChannelId, relayInfo.OriginModelName, relayInfo.IsStream))

	// get & validate textRequest 获取并验证文本请求
	textRequest, err := getAndValidateClaudeRequest(c)
	if err != nil {
		common.LogError(c, fmt.Sprintf("[CLAUDE] Request validation failed | Error:%s", err.Error()))
		return types.NewError(err, types.ErrorCodeInvalidRequest)
	}

	common.LogInfo(c, fmt.Sprintf("[CLAUDE] Request validated | Messages:%d | MaxTokens:%d | Stream:%v", 
		len(textRequest.Messages), textRequest.MaxTokens, textRequest.Stream))

	if textRequest.Stream {
		relayInfo.IsStream = true
	}

	err = helper.ModelMappedHelper(c, relayInfo, textRequest)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError)
	}

	// [CLAUDE] Token计算开始
	tokenCountStart := time.Now()
	promptTokens, err := getClaudePromptTokens(textRequest, relayInfo)
	tokenCountTime := time.Since(tokenCountStart)
	// count messages token error 计算promptTokens错误
	if err != nil {
		common.LogError(c, fmt.Sprintf("[CLAUDE] Token count failed | Error:%s | Time:%v", err.Error(), tokenCountTime))
		return types.NewError(err, types.ErrorCodeCountTokenFailed)
	}

	common.LogInfo(c, fmt.Sprintf("[CLAUDE] Token counted | PromptTokens:%d | Time:%v", promptTokens, tokenCountTime))

	priceData, err := helper.ModelPriceHelper(c, relayInfo, promptTokens, int(textRequest.MaxTokens))
	if err != nil {
		return types.NewError(err, types.ErrorCodeModelPriceError)
	}

	// pre-consume quota 预消耗配额
	preConsumedQuota, userQuota, newAPIError := preConsumeQuota(c, priceData.ShouldPreConsumedQuota, relayInfo)

	if newAPIError != nil {
		return newAPIError
	}
	defer func() {
		if newAPIError != nil {
			returnPreConsumedQuota(c, relayInfo, userQuota, preConsumedQuota)
		}
	}()

	adaptor := GetAdaptor(relayInfo.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", relayInfo.ApiType), types.ErrorCodeInvalidApiType)
	}
	adaptor.Init(relayInfo)
	var requestBody io.Reader

	if textRequest.MaxTokens == 0 {
		textRequest.MaxTokens = uint(model_setting.GetClaudeSettings().GetDefaultMaxTokens(textRequest.Model))
	}

	if model_setting.GetClaudeSettings().ThinkingAdapterEnabled &&
		strings.HasSuffix(textRequest.Model, "-thinking") {
		if textRequest.Thinking == nil {
			// 因为BudgetTokens 必须大于1024
			if textRequest.MaxTokens < 1280 {
				textRequest.MaxTokens = 1280
			}

			// BudgetTokens 为 max_tokens 的 80%
			textRequest.Thinking = &dto.Thinking{
				Type:         "enabled",
				BudgetTokens: common.GetPointer[int](int(float64(textRequest.MaxTokens) * model_setting.GetClaudeSettings().ThinkingAdapterBudgetTokensPercentage)),
			}
			// TODO: 临时处理
			// https://docs.anthropic.com/en/docs/build-with-claude/extended-thinking#important-considerations-when-using-extended-thinking
			textRequest.TopP = 0
			textRequest.Temperature = common.GetPointer[float64](1.0)
		}
		textRequest.Model = strings.TrimSuffix(textRequest.Model, "-thinking")
		relayInfo.UpstreamModelName = textRequest.Model
	}

	convertedRequest, err := adaptor.ConvertClaudeRequest(c, relayInfo, textRequest)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed)
	}
	jsonData, err := common.Marshal(convertedRequest)
	if common.DebugEnabled {
		println("requestBody: ", string(jsonData))
	}
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed)
	}
	requestBody = bytes.NewBuffer(jsonData)

	statusCodeMappingStr := c.GetString("status_code_mapping")
	// [CLAUDE] 准备上游API调用
	requestSize := len(jsonData)
	common.LogInfo(c, fmt.Sprintf("[CLAUDE] Calling upstream API | URL:%s | RequestSize:%d bytes | Model:%s", 
		relayInfo.BaseUrl, requestSize, relayInfo.UpstreamModelName))
	
	upstreamCallStart := time.Now()
	var httpResp *http.Response
	resp, err := adaptor.DoRequest(c, relayInfo, requestBody)
	upstreamCallTime := time.Since(upstreamCallStart)
	
	if err != nil {
		common.LogError(c, fmt.Sprintf("[CLAUDE] Upstream API call failed | Error:%s | Time:%v", err.Error(), upstreamCallTime))
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	if resp != nil {
		httpResp = resp.(*http.Response)
		relayInfo.IsStream = relayInfo.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")
		
		// [CLAUDE] 记录上游API响应信息
		contentType := httpResp.Header.Get("Content-Type")
		contentLength := httpResp.Header.Get("Content-Length")
		common.LogInfo(c, fmt.Sprintf("[CLAUDE] Upstream API response | Status:%d | ContentType:%s | ContentLength:%s | Time:%v", 
			httpResp.StatusCode, contentType, contentLength, upstreamCallTime))
		
		if httpResp.StatusCode != http.StatusOK {
			common.LogWarn(c, fmt.Sprintf("[CLAUDE] Upstream API error status | Status:%d | Time:%v", 
				httpResp.StatusCode, upstreamCallTime))
			newAPIError = service.RelayErrorHandler(c, httpResp, false)
			// reset status code 重置状态码
			service.ResetStatusCode(newAPIError, statusCodeMappingStr)
			return newAPIError
		}
	}

	// [CLAUDE] 开始响应处理
	responseProcessStart := time.Now()
	usage, newAPIError := adaptor.DoResponse(c, httpResp, relayInfo)
	responseProcessTime := time.Since(responseProcessStart)
	
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		common.LogError(c, fmt.Sprintf("[CLAUDE] Response processing failed | Error:%s | Time:%v", 
			newAPIError.Error(), responseProcessTime))
		return newAPIError
	}
	
	// [CLAUDE] 记录最终使用情况
	totalTime := time.Since(startTime)
	if usage != nil {
		usageInfo := usage.(*dto.Usage)
		common.LogInfo(c, fmt.Sprintf("[CLAUDE] Request completed | TotalTime:%v | PromptTokens:%d | CompletionTokens:%d | TotalTokens:%d", 
			totalTime, usageInfo.PromptTokens, usageInfo.CompletionTokens, usageInfo.TotalTokens))
	} else {
		common.LogInfo(c, fmt.Sprintf("[CLAUDE] Request completed | TotalTime:%v | Usage:nil", totalTime))
	}
	
	service.PostClaudeConsumeQuota(c, relayInfo, usage.(*dto.Usage), preConsumedQuota, userQuota, priceData, "")
	return nil
}

func getClaudePromptTokens(textRequest *dto.ClaudeRequest, info *relaycommon.RelayInfo) (int, error) {
	var promptTokens int
	var err error
	switch info.RelayMode {
	default:
		promptTokens, err = service.CountTokenClaudeRequest(*textRequest, info.UpstreamModelName)
	}
	info.PromptTokens = promptTokens
	return promptTokens, err
}
