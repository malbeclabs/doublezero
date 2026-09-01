use std::{env, fs};

use anyhow::{Context, Result, bail};
use reqwest::{
    Body, Client, RequestBuilder,
    header::{ACCEPT, AUTHORIZATION, CONTENT_TYPE},
};
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

pub fn build_message_request(
    client: &Client,
    body: Body,
    webhook: String,
) -> Result<RequestBuilder> {
    let msg_request = client
        .post(webhook)
        .header(ACCEPT, "application/json")
        .body(body);
    Ok(msg_request)
}

pub async fn upload_file(filepath: String, channel_id: String) -> anyhow::Result<Option<String>> {
    let created_csv = fs::metadata(filepath.clone())?;
    let file_size = created_csv.len();
    let client = reqwest::Client::new();

    let file_upload_url = get_file_upload_url(&client, &filepath, file_size).await?;

    upload_file_bytes(&client, &filepath, &file_upload_url.upload_url).await?;

    let complete_file_upload_response =
        complete_file_upload(&client, filepath, file_upload_url.file_id, channel_id).await?;

    // There should only be one file uploaded
    let permalink = complete_file_upload_response
        .files
        .into_iter()
        .next()
        .map(|file| file.permalink);

    Ok(permalink)
}

pub fn build_multi_row_table(
    header: String,
    table_headers: Vec<String>,
    table_values: Vec<Vec<String>>,
) -> Result<SlackMessage> {
    let mut body: Vec<Block> = Vec::new();

    let header = Block {
        column_settings: None,
        block_type: "header".to_string(),
        fields: None,
        rows: None,
        text: Some(Text {
            text_type: "plain_text".to_string(),
            text: Some(header),
            emoji: Some(true),
        }),
    };

    body.push(header);

    let mut rows: Vec<Vec<Text>> = Vec::with_capacity(table_values.len() + 1);

    let header_row: Vec<Text> = table_headers
        .into_iter()
        .map(|th| Text {
            text_type: "raw_text".to_string(),
            text: Some(th),
            emoji: None,
        })
        .collect();

    rows.push(header_row);

    for row_values in table_values {
        let row: Vec<Text> = row_values
            .into_iter()
            .map(|tv| Text {
                text_type: "raw_text".to_string(),
                text: Some(tv),
                emoji: None,
            })
            .collect();
        rows.push(row);
    }

    let table = Block {
        column_settings: Some(vec![ColumnSetting {
            is_wrapped: true,
            align: "left".to_string(),
        }]),
        rows: Some(rows),
        block_type: "table".to_string(),
        fields: None,
        text: None,
    };

    body.push(table);

    Ok(SlackMessage { blocks: body })
}

pub fn build_table(
    header: String,
    table_headers: Vec<String>,
    table_values: Vec<String>,
) -> Result<SlackMessage> {
    let mut body: Vec<Block> = Vec::new();

    let header = Block {
        column_settings: None,
        block_type: "header".to_string(),
        fields: None,
        rows: None,
        text: Some(Text {
            text_type: "plain_text".to_string(),
            text: Some(header),
            emoji: Some(true),
        }),
    };
    body.push(header);

    let mut table_header: Vec<Text> = Vec::with_capacity(table_headers.len());
    for th in table_headers {
        let header = Text {
            text_type: "raw_text".to_string(),
            text: Some(th),
            emoji: None,
        };
        table_header.push(header)
    }

    let mut table_rows: Vec<Text> = Vec::with_capacity(table_values.len());
    for tv in table_values {
        let row = Text {
            text_type: "raw_text".to_string(),
            text: Some(tv),
            emoji: None,
        };
        table_rows.push(row)
    }

    let table = Block {
        column_settings: Some(vec![ColumnSetting {
            is_wrapped: true,
            align: "left".to_string(),
        }]),
        rows: Some(vec![table_header, table_rows]),
        block_type: "table".to_string(),
        fields: None,
        text: None,
    };
    body.push(table);

    let slack_message = SlackMessage { blocks: body };
    Ok(slack_message)
}

async fn complete_file_upload(
    client: &Client,
    filename: String,
    file_id: String,
    channel_id: String,
) -> anyhow::Result<FileCompleteResponse> {
    let complete_file_upload_url = "https://slack.com/api/files.completeUploadExternal";
    let file_upload_body = FileUploadRequest {
        files: vec![FileCompleteRequest {
            id: file_id,
            title: filename,
        }],
        channel_id,
    };

    let response = client
        .post(complete_file_upload_url)
        .header(AUTHORIZATION, format!("Bearer {}", slack_access_token()?))
        .header("Content-Type", "application/json; charset=utf-8")
        .json(&file_upload_body)
        .send()
        .await?
        .json::<FileCompleteResponse>()
        .await?;

    Ok(response)
}

async fn upload_file_bytes(
    client: &Client,
    filename: &str,
    file_upload_url: &str,
) -> anyhow::Result<()> {
    let file_bytes = fs::read(filename)?;

    let response = client
        .put(file_upload_url)
        .header(CONTENT_TYPE, "application/octet-stream")
        .body(file_bytes)
        .send()
        .await
        .context("Failed to upload {filename}")?;

    println!("CSV upload: {}", response.status());
    Ok(())
}

async fn get_file_upload_url(
    client: &Client,
    filename: &str,
    length: u64,
) -> anyhow::Result<GetFileUploadUrlResponse> {
    let get_file_upload_url = "https://slack.com/api/files.getUploadURLExternal";
    let file_upload_body = GetFileUploadUrl {
        filename: filename.to_string(),
        length,
    };
    let request = client
        .post(get_file_upload_url)
        .header(CONTENT_TYPE, "application/x-www-form-urlencoded")
        .header(AUTHORIZATION, format!("Bearer {}", slack_access_token()?))
        .form(&file_upload_body);

    let resp = request
        .send()
        .await?
        .json::<GetFileUploadUrlResponse>()
        .await?;

    Ok(resp)
}

fn slack_access_token() -> Result<String> {
    match env::var("SLACK_ACCESS_TOKEN") {
        Ok(token) => Ok(token),
        Err(_) => bail!("SLACK_ACCESS_TOKEN env var not set"),
    }
}
