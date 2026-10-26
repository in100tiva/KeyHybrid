# Registro de Projetos

> Registro canônico exigido pela regra global do kit-mcp.
> Campos marcados com (obrigatório) não podem ficar vazios.

## Projeto principal: KeyHybrid

- **Pasta local do projeto** (obrigatório): `/home/tech-lead/Documentos/DEV/KeyHybrid`
- **Repositório do projeto** (obrigatório): https://github.com/in100tiva/KeyHybrid
- **Documentação local** (obrigatório): `/home/tech-lead/Documentos/DEV/KeyHybrid/docs`
- **Repositório da documentação** (opcional): —
- **Infra / VPS** (opcional): local, sem VPS. Gateway em `127.0.0.1:8090` como serviço `systemd --user` (`keyhybrid.service`); cofre em `~/.keyhybrid/pb_data`; binário em `~/.keyhybrid/bin/keyhybrid-server` (`kh` em `~/.local/bin`).
- **Notas** (opcional): cofre de chaves de API/serviço usado pelos agentes via `kh run --env VAR=alias -- <cmd>` (aliases atuais: `openrouter`, `supabase`). Escrita no cofre exige o token local `~/.keyhybrid/.cli.token`.

## Projetos conectados

<!-- Cada projeto adicional DEVE preencher todos os campos obrigatórios. -->
