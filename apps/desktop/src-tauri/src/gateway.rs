use crate::vault::{assert_local_origin, has_vault_pin, is_unlocked, SecretHolder};
use reqwest::Client;
use serde::{Deserialize, Serialize};
use std::path::PathBuf;
use std::time::Duration;

#[derive(Debug, Serialize, Deserialize)]
pub struct HealthStatus {
    pub status: String,
    pub app: String,
    pub timestamp: String,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct KeySafeView {
    pub id: String,
    pub alias: String,
    pub provider: String,
    pub masked_key: String,
    pub virtual_token: String,
    pub default_model: String,
    pub active: bool,
    #[serde(default)]
    pub updated: String,
    #[serde(default)]
    pub kind: Option<String>,
    #[serde(default)]
    pub base_url: Option<String>,
}

#[derive(Debug, Serialize, Deserialize)]
struct KeyCreatePayload {
    pub alias: String,
    pub provider: String,
    pub secret_key: String,
    pub default_model: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub kind: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub base_url: Option<String>,
}

/// Token local do gateway (mesmo arquivo que o `kh run` lê). Rotas de escrita exigem o header.
fn cli_token() -> Result<String, String> {
    const AUSENTE: &str = "Token local ausente: abra o gateway do KeyHybrid uma vez";
    let home = std::env::var("HOME")
        .or_else(|_| std::env::var("USERPROFILE"))
        .map_err(|_| AUSENTE.to_string())?;
    std::fs::read_to_string(PathBuf::from(home).join(".keyhybrid").join(".cli.token"))
        .ok()
        .map(|t| t.trim().to_string())
        .filter(|t| !t.is_empty())
        .ok_or_else(|| AUSENTE.to_string())
}

#[tauri::command]
pub async fn check_gateway_health(gateway_url: String) -> Result<HealthStatus, String> {
    let client = Client::builder()
        .timeout(Duration::from_secs(3))
        .build()
        .map_err(|e| e.to_string())?;

    let url = format!("{}/api/keyhybrid/health", gateway_url.trim_end_matches('/'));

    let resp = match client.get(&url).send().await {
        Ok(r) => r,
        Err(_) => {
            crate::ensure_gateway_running();
            tokio::time::sleep(Duration::from_millis(700)).await;
            client
                .get(&url)
                .send()
                .await
                .map_err(|e| format!("Gateway offline: {e}"))?
        }
    };

    if !resp.status().is_success() {
        return Err(format!("HTTP {}", resp.status()));
    }

    resp.json::<HealthStatus>()
        .await
        .map_err(|e| format!("Resposta inválida: {e}"))
}

#[tauri::command]
pub async fn list_vault_keys(gateway_url: String) -> Result<Vec<KeySafeView>, String> {
    let client = Client::builder()
        .timeout(Duration::from_secs(5))
        .build()
        .map_err(|e| e.to_string())?;

    let url = format!("{}/api/keyhybrid/keys", gateway_url.trim_end_matches('/'));

    let resp = client
        .get(&url)
        .send()
        .await
        .map_err(|e| format!("Falha ao conectar ao Gateway: {e}"))?;

    if !resp.status().is_success() {
        return Err(format!("Erro HTTP {}", resp.status()));
    }

    resp.json::<Vec<KeySafeView>>()
        .await
        .map_err(|e| format!("Erro ao ler chaves: {e}"))
}

#[tauri::command]
pub async fn save_vault_key(
    window: tauri::WebviewWindow,
    gateway_url: String,
    alias: String,
    provider: String,
    secret_key: String,
    default_model: String,
    kind: Option<String>,
    base_url: Option<String>,
) -> Result<KeySafeView, String> {
    assert_local_origin(&window)?;
    let clean_alias = alias.trim().to_string();
    let holder = SecretHolder {
        secret: secret_key.trim().to_string(),
    };

    if clean_alias.is_empty() || holder.secret.is_empty() {
        return Err("Informe um nome para a chave e o valor da chave secreta.".to_string());
    }

    if has_vault_pin().unwrap_or(false) && !is_unlocked() {
        return Err("Cofre bloqueado. Digite seu PIN de 6 dígitos para autorizar salvar chaves.".to_string());
    }

    let token = cli_token()?;

    // 1. Registrar no Gateway Go local para disponibilizar em http://127.0.0.1:8090/v1
    let client = Client::builder()
        .timeout(Duration::from_secs(5))
        .build()
        .map_err(|e| e.to_string())?;

    let url = format!("{}/api/keyhybrid/keys", gateway_url.trim_end_matches('/'));
    let payload = KeyCreatePayload {
        alias: clean_alias,
        provider,
        secret_key: holder.secret.clone(),
        default_model,
        kind,
        base_url,
    };

    let resp = client
        .post(&url)
        .header("X-KeyHybrid-Token", token.as_str())
        .json(&payload)
        .send()
        .await
        .map_err(|e| format!("O Gateway Go está offline ou inacessível: {e}"))?;

    if resp.status() == reqwest::StatusCode::UNAUTHORIZED {
        return Err("Token local recusado: reinicie o gateway do KeyHybrid".to_string());
    }

    if !resp.status().is_success() {
        let body = resp.text().await.unwrap_or_default();
        return Err(format!("Erro no Gateway: {body}"));
    }

    resp.json::<KeySafeView>()
        .await
        .map_err(|e| format!("Erro ao decodificar resposta: {e}"))
}

#[tauri::command]
pub async fn activate_vault_key(window: tauri::WebviewWindow, gateway_url: String, id: String) -> Result<(), String> {
    assert_local_origin(&window)?;
    let token = cli_token()?;

    let client = Client::builder()
        .timeout(Duration::from_secs(5))
        .build()
        .map_err(|e| e.to_string())?;

    let url = format!(
        "{}/api/keyhybrid/keys/{}/activate",
        gateway_url.trim_end_matches('/'),
        id
    );

    let resp = client
        .post(&url)
        .header("X-KeyHybrid-Token", token.as_str())
        .send()
        .await
        .map_err(|e| format!("Falha ao ativar chave: {e}"))?;

    if resp.status() == reqwest::StatusCode::UNAUTHORIZED {
        return Err("Token local recusado: reinicie o gateway do KeyHybrid".to_string());
    }

    if !resp.status().is_success() {
        return Err(format!("Erro HTTP {}", resp.status()));
    }

    Ok(())
}

#[tauri::command]
pub async fn delete_vault_key(
    window: tauri::WebviewWindow,
    gateway_url: String,
    id: String,
    _alias: String,
) -> Result<(), String> {
    assert_local_origin(&window)?;
    if has_vault_pin().unwrap_or(false) && !is_unlocked() {
        return Err("Cofre bloqueado. Digite seu PIN de 6 dígitos para autorizar excluir chaves.".to_string());
    }

    let token = cli_token()?;

    let client = Client::builder()
        .timeout(Duration::from_secs(5))
        .build()
        .map_err(|e| e.to_string())?;

    let url = format!(
        "{}/api/keyhybrid/keys/{}",
        gateway_url.trim_end_matches('/'),
        id
    );

    let resp = client
        .delete(&url)
        .header("X-KeyHybrid-Token", token.as_str())
        .send()
        .await
        .map_err(|e| format!("Falha ao excluir do Gateway: {e}"))?;

    if resp.status() == reqwest::StatusCode::UNAUTHORIZED {
        return Err("Token local recusado: reinicie o gateway do KeyHybrid".to_string());
    }

    if !resp.status().is_success() {
        return Err(format!("Erro HTTP {}", resp.status()));
    }

    Ok(())
}
