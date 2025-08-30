package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"one-api/common"
	"one-api/dto"
	"one-api/types"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func MidjourneyErrorWrapper(code int, desc string) *dto.MidjourneyResponse {
	return &dto.MidjourneyResponse{
		Code:        code,
		Description: desc,
	}
}

func MidjourneyErrorWithStatusCodeWrapper(code int, desc string, statusCode int) *dto.MidjourneyResponseWithStatusCode {
	return &dto.MidjourneyResponseWithStatusCode{
		StatusCode: statusCode,
		Response:   *MidjourneyErrorWrapper(code, desc),
	}
}

//// OpenAIErrorWrapper wraps an error into an OpenAIErrorWithStatusCode
//func OpenAIErrorWrapper(err error, code string, statusCode int) *dto.OpenAIErrorWithStatusCode {
//	text := err.Error()
//	lowerText := strings.ToLower(text)
//	if !strings.HasPrefix(lowerText, "get file base64 from url") && !strings.HasPrefix(lowerText, "mime type is not supported") {
//		if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
//			common.SysLog(fmt.Sprintf("error: %s", text))
//			text = "请求上游地址失败"
//		}
//	}
//	openAIError := dto.OpenAIError{
//		Message: text,
//		Type:    "new_api_error",
//		Code:    code,
//	}
//	return &dto.OpenAIErrorWithStatusCode{
//		Error:      openAIError,
//		StatusCode: statusCode,
//	}
//}
//
//func OpenAIErrorWrapperLocal(err error, code string, statusCode int) *dto.OpenAIErrorWithStatusCode {
//	openaiErr := OpenAIErrorWrapper(err, code, statusCode)
//	openaiErr.LocalError = true
//	return openaiErr
//}

func ClaudeErrorWrapper(err error, code string, statusCode int) *dto.ClaudeErrorWithStatusCode {
	text := err.Error()
	lowerText := strings.ToLower(text)
	if !strings.HasPrefix(lowerText, "get file base64 from url") {
		if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
			common.SysLog(fmt.Sprintf("error: %s", text))
			text = "请求上游地址失败"
		}
	}
	claudeError := dto.ClaudeError{
		Message: text,
		Type:    "new_api_error",
	}
	return &dto.ClaudeErrorWithStatusCode{
		Error:      claudeError,
		StatusCode: statusCode,
	}
}

func ClaudeErrorWrapperLocal(err error, code string, statusCode int) *dto.ClaudeErrorWithStatusCode {
	claudeErr := ClaudeErrorWrapper(err, code, statusCode)
	claudeErr.LocalError = true
	return claudeErr
}

// RelayErrorHandler 处理上游API错误响应（带上下文的新版本）
func RelayErrorHandler(c *gin.Context, resp *http.Response, showBodyWhenFail bool) (newApiErr *types.NewAPIError) {
	// [CLAUDE] 上游错误处理开始
	common.LogWarn(c, fmt.Sprintf("[CLAUDE] Upstream error detected | Status:%d | URL:%s", 
		resp.StatusCode, resp.Request.URL.String()))
	
	newApiErr = &types.NewAPIError{
		StatusCode: resp.StatusCode,
		ErrorType:  types.ErrorTypeOpenAIError,
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		common.LogError(c, fmt.Sprintf("[CLAUDE] Failed to read error response body | Error:%s", err.Error()))
		return
	}
	common.CloseResponseBodyGracefully(resp)
	
	// [CLAUDE] 记录原始错误响应
	bodyStr := string(responseBody)
	if len(bodyStr) > 1000 {
		bodyStr = bodyStr[:1000] + "...[truncated]"
	}
	common.LogError(c, fmt.Sprintf("[CLAUDE] Upstream error response | Body:%s", bodyStr))
	
	var errResponse dto.GeneralErrorResponse
	err = common.Unmarshal(responseBody, &errResponse)
	if err != nil {
		common.LogError(c, fmt.Sprintf("[CLAUDE] Failed to parse error response | ParseError:%s", err.Error()))
		if showBodyWhenFail {
			newApiErr.Err = fmt.Errorf("bad response status code %d, body: %s", resp.StatusCode, string(responseBody))
		} else {
			newApiErr.Err = fmt.Errorf("bad response status code %d", resp.StatusCode)
		}
		return
	}
	if errResponse.Error.Message != "" {
		// General format error (OpenAI, Anthropic, Gemini, etc.)
		common.LogError(c, fmt.Sprintf("[CLAUDE] Structured error response | Type:%s | Code:%s | Message:%s", 
			errResponse.Error.Type, errResponse.Error.Code, errResponse.Error.Message))
		newApiErr = types.WithOpenAIError(errResponse.Error, resp.StatusCode)
	} else {
		common.LogError(c, fmt.Sprintf("[CLAUDE] Unstructured error response | Message:%s", errResponse.ToMessage()))
		newApiErr = types.NewErrorWithStatusCode(errors.New(errResponse.ToMessage()), types.ErrorCodeBadResponseStatusCode, resp.StatusCode)
		newApiErr.ErrorType = types.ErrorTypeOpenAIError
	}
	
	// [CLAUDE] 错误处理完成日志
	common.LogError(c, fmt.Sprintf("[CLAUDE] Upstream error processing completed | FinalError:%s", newApiErr.Error()))
	return
}

// RelayErrorHandlerLegacy 处理上游API错误响应（兼容旧版本，无上下文）
func RelayErrorHandlerLegacy(resp *http.Response, showBodyWhenFail bool) (newApiErr *types.NewAPIError) {
	newApiErr = &types.NewAPIError{
		StatusCode: resp.StatusCode,
		ErrorType:  types.ErrorTypeOpenAIError,
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	common.CloseResponseBodyGracefully(resp)
	var errResponse dto.GeneralErrorResponse

	err = common.Unmarshal(responseBody, &errResponse)
	if err != nil {
		if showBodyWhenFail {
			newApiErr.Err = fmt.Errorf("bad response status code %d, body: %s", resp.StatusCode, string(responseBody))
		} else {
			newApiErr.Err = fmt.Errorf("bad response status code %d", resp.StatusCode)
		}
		return
	}
	if errResponse.Error.Message != "" {
		// General format error (OpenAI, Anthropic, Gemini, etc.)
		newApiErr = types.WithOpenAIError(errResponse.Error, resp.StatusCode)
	} else {
		newApiErr = types.NewErrorWithStatusCode(errors.New(errResponse.ToMessage()), types.ErrorCodeBadResponseStatusCode, resp.StatusCode)
		newApiErr.ErrorType = types.ErrorTypeOpenAIError
	}
	return
}

func ResetStatusCode(newApiErr *types.NewAPIError, statusCodeMappingStr string) {
	if statusCodeMappingStr == "" || statusCodeMappingStr == "{}" {
		return
	}
	statusCodeMapping := make(map[string]string)
	err := json.Unmarshal([]byte(statusCodeMappingStr), &statusCodeMapping)
	if err != nil {
		return
	}
	if newApiErr.StatusCode == http.StatusOK {
		return
	}
	codeStr := strconv.Itoa(newApiErr.StatusCode)
	if _, ok := statusCodeMapping[codeStr]; ok {
		intCode, _ := strconv.Atoi(statusCodeMapping[codeStr])
		newApiErr.StatusCode = intCode
	}
}

func TaskErrorWrapperLocal(err error, code string, statusCode int) *dto.TaskError {
	openaiErr := TaskErrorWrapper(err, code, statusCode)
	openaiErr.LocalError = true
	return openaiErr
}

func TaskErrorWrapper(err error, code string, statusCode int) *dto.TaskError {
	text := err.Error()
	lowerText := strings.ToLower(text)
	if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
		common.SysLog(fmt.Sprintf("error: %s", text))
		text = "请求上游地址失败"
	}
	//避免暴露内部错误
	taskError := &dto.TaskError{
		Code:       code,
		Message:    text,
		StatusCode: statusCode,
		Error:      err,
	}

	return taskError
}
