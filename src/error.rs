use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use axum::Json;
use serde_json::json;
use thiserror::Error;

#[derive(Error, Debug)]
pub enum ContextdError {
    #[error("Database error: {0}")]
    Database(#[from] sqlx::Error),

    #[error("Search engine error: {0}")]
    Search(String),

    #[error("Configuration error: {0}")]
    Config(#[from] anyhow::Error),

    #[error("Serialization error: {0}")]
    Serialization(#[from] serde_json::Error),

    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),

    #[error("Validation error: {0}")]
    Validation(String),

    #[error("Not found: {0}")]
    NotFound(String),

    #[error("Unauthorized: {0}")]
    Unauthorized(String),

    #[error("Rate limit exceeded")]
    RateLimitExceeded,

    #[error("Request too large: {0}")]
    RequestTooLarge(String),

    #[error("Internal server error: {0}")]
    Internal(String),
}

impl From<std::string::FromUtf8Error> for ContextdError {
    fn from(err: std::string::FromUtf8Error) -> Self {
        ContextdError::Internal(format!("UTF-8 conversion error: {}", err))
    }
}

impl From<uuid::Error> for ContextdError {
    fn from(err: uuid::Error) -> Self {
        ContextdError::Internal(format!("UUID error: {}", err))
    }
}

impl From<chrono::ParseError> for ContextdError {
    fn from(err: chrono::ParseError) -> Self {
        ContextdError::Internal(format!("Date parse error: {}", err))
    }
}

impl IntoResponse for ContextdError {
    fn into_response(self) -> Response {
        let (status, error_message) = match self {
            ContextdError::Database(ref e) => {
                tracing::error!("Database error: {}", e);
                (StatusCode::INTERNAL_SERVER_ERROR, "Database error")
            }
            ContextdError::Search(ref e) => {
                tracing::error!("Search error: {}", e);
                (StatusCode::INTERNAL_SERVER_ERROR, "Search error")
            }
            ContextdError::Config(ref e) => {
                tracing::error!("Configuration error: {}", e);
                (StatusCode::INTERNAL_SERVER_ERROR, "Configuration error")
            }
            ContextdError::Serialization(ref e) => {
                tracing::error!("Serialization error: {}", e);
                (StatusCode::BAD_REQUEST, "Invalid request format")
            }
            ContextdError::Io(ref e) => {
                tracing::error!("IO error: {}", e);
                (StatusCode::INTERNAL_SERVER_ERROR, "IO error")
            }
            ContextdError::Validation(ref e) => {
                tracing::warn!("Validation error: {}", e);
                (StatusCode::BAD_REQUEST, e.as_str())
            }
            ContextdError::NotFound(ref e) => {
                tracing::info!("Not found: {}", e);
                (StatusCode::NOT_FOUND, e.as_str())
            }
            ContextdError::Unauthorized(ref e) => {
                tracing::warn!("Unauthorized: {}", e);
                (StatusCode::UNAUTHORIZED, e.as_str())
            }
            ContextdError::RateLimitExceeded => {
                tracing::warn!("Rate limit exceeded");
                (StatusCode::TOO_MANY_REQUESTS, "Rate limit exceeded")
            }
            ContextdError::RequestTooLarge(ref e) => {
                tracing::warn!("Request too large: {}", e);
                (StatusCode::PAYLOAD_TOO_LARGE, e.as_str())
            }
            ContextdError::Internal(ref e) => {
                tracing::error!("Internal error: {}", e);
                (StatusCode::INTERNAL_SERVER_ERROR, "Internal server error")
            }
        };

        let body = Json(json!({
            "error": {
                "message": error_message,
                "type": match self {
                    ContextdError::Database(_) => "database_error",
                    ContextdError::Search(_) => "search_error",
                    ContextdError::Config(_) => "config_error",
                    ContextdError::Serialization(_) => "serialization_error",
                    ContextdError::Io(_) => "io_error",
                    ContextdError::Validation(_) => "validation_error",
                    ContextdError::NotFound(_) => "not_found",
                    ContextdError::Unauthorized(_) => "unauthorized",
                    ContextdError::RateLimitExceeded => "rate_limit_exceeded",
                    ContextdError::RequestTooLarge(_) => "request_too_large",
                    ContextdError::Internal(_) => "internal_error",
                }
            }
        }));

        (status, body).into_response()
    }
}

pub type Result<T> = std::result::Result<T, ContextdError>;
