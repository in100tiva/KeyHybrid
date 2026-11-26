package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/tabwriter"
)

// Só o que o agente precisa para montar o kh run: nunca segredo, máscara, base_url nem cabeçalho
type khListItem struct {
	Alias        string `json:"alias"`
	Kind         string `json:"kind"`
	Provider     string `json:"provider"`
	DefaultModel string `json:"default_model"`
}

// kh list [--json]: aliases do cofre pelo GET /api/keyhybrid/keys com o token local
func listCmd(args []string, out io.Writer) int {
	asJSON := false
	for _, a := range args {
		if a != "--json" {
			fmt.Fprintln(os.Stderr, "kh list: opção desconhecida:", a)
			fmt.Fprintln(os.Stderr, "uso: kh list [--json]")
			return 2
		}
		asJSON = true
	}
	gw, token, code := gatewayAndToken("kh list")
	if code != 0 {
		return code
	}
	req, err := http.NewRequest(http.MethodGet, gw+"/api/keyhybrid/keys", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kh list: gateway inválido")
		return 2
	}
	req.Header.Set("X-KeyHybrid-Token", token)
	resp, err := khClient().Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kh list: KeyHybrid não está respondendo em", gw)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "kh list: gateway respondeu HTTP", resp.StatusCode)
		return 1
	}
	// Decodifica direto nos campos permitidos: o resto da resposta é descartado
	items := []khListItem{}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&items) != nil {
		fmt.Fprintln(os.Stderr, "kh list: resposta inesperada do gateway")
		return 1
	}

	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(items)
		return 0
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ALIAS\tTIPO\tPROVEDOR\tMODELO PADRÃO")
	for _, it := range items {
		tipo, modelo := "IA", it.DefaultModel
		if it.Kind == "service" {
			tipo, modelo = "serviço", "—"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", it.Alias, tipo, it.Provider, modelo)
	}
	_ = tw.Flush()
	return 0
}
