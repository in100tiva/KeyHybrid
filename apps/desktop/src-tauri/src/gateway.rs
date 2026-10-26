use crate::vault::{remove_from_dpapi, store_in_dpapi, SecretHolder};
use reqwest::Client;
use serde::{Deserialize, Serialize};
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
    gateway_url: String,
    alias: String,
    provider: String,
    secret_key: String,
    default_model: String,
    kind: Option<String>,
    base_url: Option<String>,
) -> Result<KeySafeView, String> {
    let clean_alias = alias.trim().to_string();
    let holder = SecretHolder {
        secret: secret_key.trim().to_string(),
    };

    if clean_alias.is_empty() || holder.secret.is_empty() {
        return Err("Informe um nome para a chave e o valor da chave secreta.".to_string());
    }

    // 1. Gravar no cofre nativo do Windows (DPAPI) com limpeza de RAM via ZeroizeOnDrop
    let _ = store_in_dpapi(&clean_alias, &holder.secret);

    // 2. Registrar no Gateway Go local para disponibilizar em http://127.0.0.1:8090/v1
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
        .json(&payload)
        .send()
        .await
        .map_err(|e| format!("Salva no DPAPI, mas o Gateway Go está offline: {e}"))?;

    if !resp.status().is_success() {
        let body = resp.text().await.unwrap_or_default();
        return Err(format!("Erro no Gateway: {body}"));
    }

    resp.json::<KeySafeView>()
        .await
        .map_err(|e| format!("Erro ao decodificar resposta: {e}"))
}

#[tauri::command]
pub async fn activate_vault_key(gateway_url: String, id: String) -> Result<(), String> {
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
        .send()
        .await
        .map_err(|e| format!("Falha ao ativar chave: {e}"))?;

    if !resp.status().is_success() {
        return Err(format!("Erro HTTP {}", resp.status()));
    }

    Ok(())
}

#[tauri::command]
pub async fn delete_vault_key(
    gateway_url: String,
    id: String,
    alias: String,
) -> Result<(), String> {
    remove_from_dpapi(&alias);

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
        .send()
        .await
        .map_err(|e| format!("Falha ao excluir do Gateway: {e}"))?;

    if !resp.status().is_success() {
        return Err(format!("Erro HTTP {}", resp.status()));
    }

    Ok(())
}
