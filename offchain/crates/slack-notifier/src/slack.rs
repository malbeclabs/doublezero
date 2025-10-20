use serde::{Deserialize, Serialize};

#[derive(Debug, Serialize)]
pub struct SlackMessage {
    pub blocks: Vec<Block>,
}

#[derive(Debug, Serialize)]
pub struct Block {
    #[serde(rename = "type")]
    pub block_type: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub text: Option<Text>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub fields: Option<Vec<Text>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub column_settings: Option<Vec<ColumnSetting>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub rows: Option<Vec<Vec<Text>>>,
}

#[derive(Debug, Serialize)]
pub struct ColumnSetting {
    pub is_wrapped: bool,
    pub align: String,
}

#[derive(Debug, Serialize)]
pub struct Text {
    #[serde(rename = "type")]
    pub text_type: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub text: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub emoji: Option<bool>,
}

#[derive(Debug, Serialize)]
pub struct GetFileUploadUrl {
    pub length: u64,
    pub token: String,
    pub filename: String,
}

#[derive(Debug, Deserialize)]
pub struct GetFileUploadUrlResponse {
    pub ok: bool,
    pub upload_url: String,
    pub file_id: String,
}

#[derive(Debug, Serialize)]
pub struct FileUploadRequest {
    pub files: Vec<FileCompleteRequest>,
    pub channel_id: String,
}

#[derive(Debug, Serialize)]
pub struct FileCompleteRequest {
    pub id: String,
    pub title: String,
}

#[derive(Debug, Deserialize)]
pub struct FileCompleteResponse {
    pub ok: bool,
    pub files: Vec<UploadedFile>,
}

#[derive(Debug, Deserialize)]
pub struct UploadedFile {
    pub permalink: String,
    pub timestamp: u64,
    pub permalink_public: String,
}
