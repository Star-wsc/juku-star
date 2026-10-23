use std::collections::HashMap;
use std::sync::Mutex;

use serde::{Deserialize, Serialize};
use tauri::Manager;

#[derive(Default)]
struct HttpState {
    client: Mutex<Option<reqwest::Client>>,
}

#[derive(Serialize, Deserialize)]
struct FetchRequest {
    method: String,
    url: String,
    headers: Option<HashMap<String, String>>,
    body: Option<String>,
}

#[derive(Serialize, Deserialize)]
struct FetchResponse {
    status: u16,
    headers: HashMap<String, String>,
    body: String,
}

fn ensure_client(state: &HttpState) -> reqwest::Client {
    let mut guard = state.client.lock().unwrap();
    if guard.is_none() {
        let jar = std::sync::Arc::new(reqwest::cookie::Jar::default());
        let client = reqwest::Client::builder()
            .cookie_provider(jar)
            .cookie_store(true)
            .timeout(std::time::Duration::from_secs(30))
            .build()
            .expect("build http client");
        *guard = Some(client);
    }
    guard.clone().unwrap()
}

#[tauri::command]
async fn http_fetch(
    state: tauri::State<'_, HttpState>,
    request: FetchRequest,
) -> Result<FetchResponse, String> {
    let client = ensure_client(&state);
    let method = reqwest::Method::from_bytes(request.method.as_bytes())
        .map_err(|e| format!("bad method: {e}"))?;

    let mut builder = client.request(method, &request.url);
    if let Some(headers) = request.headers {
        for (k, v) in headers {
            builder = builder.header(k, v);
        }
    }
    if let Some(body) = request.body {
        builder = builder
            .header("Content-Type", "application/json")
            .body(body);
    }

    let resp = builder.send().await.map_err(|e| e.to_string())?;
    let status = resp.status().as_u16();
    let mut out_headers = HashMap::new();
    for (k, v) in resp.headers().iter() {
        if let Ok(val) = v.to_str() {
            out_headers.insert(k.as_str().to_string(), val.to_string());
        }
    }
    let body = resp.text().await.map_err(|e| e.to_string())?;
    Ok(FetchResponse {
        status,
        headers: out_headers,
        body,
    })
}

#[tauri::command]
fn app_version() -> String {
    env!("CARGO_PKG_VERSION").to_string()
}

#[tauri::command]
async fn fetch_image_base64(
    state: tauri::State<'_, HttpState>,
    url: String,
) -> Result<String, String> {
    let client = ensure_client(&state);
    let resp = client.get(&url).send().await.map_err(|e| e.to_string())?;
    if !resp.status().is_success() {
        return Err(format!("image http {}", resp.status()));
    }
    let bytes = resp.bytes().await.map_err(|e| e.to_string())?;
    use base64::Engine;
    Ok(base64::engine::general_purpose::STANDARD.encode(&bytes))
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .manage(HttpState::default())
        .invoke_handler(tauri::generate_handler![
            http_fetch,
            app_version,
            fetch_image_base64
        ])
        .setup(|app| {
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.set_title("短剧库");
            }
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
