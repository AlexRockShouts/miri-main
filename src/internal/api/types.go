package api

type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return e.Message
}

func NewAPIError(code int, message string) *APIError {
	return &APIError{Code: code, Message: message}
}

type PaginationQuery struct {
	Limit  int `form:"limit" binding:"omitempty,min=1,max=1000"`
	Offset int `form:"offset" binding:"omitempty,min=0"`
}

type SessionQuery struct {
	SessionID string `form:"session_id" binding:"required"`
}

type SessionPaginationQuery struct {
	SessionID string `form:"session_id" binding:"required"`
	Limit     int    `form:"limit" binding:"omitempty,min=1,max=1000"`
	Offset    int    `form:"offset" binding:"omitempty,min=0"`
}

type PromptQuery struct {
	Prompt string `form:"prompt" binding:"required"`
	Model  string `form:"model"`
}

type InteractionRequest struct {
	Prompt    string `json:"prompt" binding:"required"`
	SessionID string `json:"session_id,omitempty"`
}

type PaginatedResponse struct {
	Data   any `json:"data"`
	Total  int `json:"total"`
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}
