package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

const khDefaultGateway = "http://127.0.0.1:8090"

// kh run -e NOME=alias [-e ...] -- comando: entrega segredos no ambiente do filho, sem imprimi-los
func runCmd(args []string) int {
	var pairs []string
	sep := false
	i := 0
	for ; i < len(args) && !sep; i++ {
		switch a := args[i]; {
		case a == "--":
			sep = true
		case a == "-e" || a == "--env":
			if i+1 >= len(args) {
				return usoRun("falta valor para " + a)
			}
			i++
			pairs = append(pairs, args[i])
		case strings.HasPrefix(a, "--env="):
			pairs = append(pairs, strings.TrimPrefix(a, "--env="))
		default:
			return usoRun("opção desconhecida: " + a)
		}
	}
	if !sep || i >= len(args) {
		return usoRun("falta -- antes do comando")
	}
	cmdArgs := args[i:]

	envNames := map[string]string{}
	var aliases []string
	seen := map[string]bool{}
	for _, p := range pairs {
		name, alias, ok := strings.Cut(p, "=")
		if !ok || !envNameRe.MatchString(name) || !aliasRe.MatchString(alias) {
			return usoRun("use NOME=alias (NOME: letras, números e _; alias: letras, números, . _ -)")
		}
		envNames[name] = alias
		if !seen[alias] {
			seen[alias] = true
			aliases = append(aliases, alias)
		}
	}

	gw := os.Getenv("KH_GATEWAY")
	if gw == "" {
		gw = khDefaultGateway
	}
	gu, err := url.Parse(gw)
	if err != nil || gu.Scheme != "http" || !hostAllowed(gu.Host) {
		fmt.Fprintln(os.Stderr, "kh run: KH_GATEWAY precisa ser http em loopback")
		return 2
	}
	tokBytes, err := os.ReadFile(cliTokenPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, "kh run: token local ausente; abra o KeyHybrid uma vez")
		return 1
	}

	secrets, code := revealSecrets(strings.TrimRight(gw, "/"), strings.TrimSpace(string(tokBytes)), aliases)
	if code != 0 {
		return code
	}

	env := os.Environ()
	var masks [][]byte
	for name, alias := range envNames {
		val := secrets[alias]
		env = append(env, name+"="+val)
		if len(val) >= 8 {
			masks = append(masks, []byte(val))
		}
	}

	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = outWriter(os.Stdout, masks)
	cmd.Stderr = outWriter(os.Stderr, masks)
	runErr := cmd.Run()
	flushMask(cmd.Stdout)
	flushMask(cmd.Stderr)
	if runErr == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		if c := ee.ExitCode(); c >= 0 {
			return c
		}
		return 1
	}
	fmt.Fprintln(os.Stderr, "kh run: não foi possível executar", cmdArgs[0])
	return 127
}

func usoRun(motivo string) int {
	fmt.Fprintln(os.Stderr, "kh run:", motivo)
	fmt.Fprintln(os.Stderr, "uso: kh run [-e NOME=alias]... -- comando [args]")
	return 2
}

// Chama o gateway local; o token vai no cabeçalho e nunca é impresso
func revealSecrets(gw, token string, aliases []string) (map[string]string, int) {
	body, _ := json.Marshal(map[string][]string{"aliases": aliases})
	req, err := http.NewRequest(http.MethodPost, gw+"/api/keyhybrid/reveal", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, "kh run: gateway inválido")
		return nil, 2
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-KeyHybrid-Token", token)
	client := &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kh run: KeyHybrid não está respondendo em", gw)
		return nil, 1
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch resp.StatusCode {
	case http.StatusOK:
		var out struct {
			Secrets map[string]string `json:"secrets"`
		}
		if json.Unmarshal(raw, &out) != nil {
			fmt.Fprintln(os.Stderr, "kh run: resposta inesperada do gateway")
			return nil, 1
		}
		return out.Secrets, 0
	case http.StatusNotFound:
		var out struct {
			Missing []string `json:"missing"`
		}
		_ = json.Unmarshal(raw, &out)
		fmt.Fprintln(os.Stderr, "kh run: alias não cadastrado:", strings.Join(out.Missing, ", "))
		return nil, 1
	case http.StatusUnauthorized:
		fmt.Fprintln(os.Stderr, "kh run: token recusado; reinicie o KeyHybrid")
		return nil, 1
	default:
		fmt.Fprintln(os.Stderr, "kh run: gateway respondeu HTTP", resp.StatusCode)
		return nil, 1
	}
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Máscara sempre ativa em stdout e stderr para evitar vazamento
func outWriter(f *os.File, secrets [][]byte) io.Writer {
	if len(secrets) == 0 {
		return f
	}
	return &maskWriter{dst: f, secrets: secrets}
}

func flushMask(w io.Writer) {
	if m, ok := w.(*maskWriter); ok {
		_ = m.Flush()
	}
}

var maskTok = []byte("****")

// Segura o sufixo que ainda pode ser começo de um segredo, para pegá-lo partido entre escritas
type maskWriter struct {
	dst     io.Writer
	secrets [][]byte
	pending []byte
}

func (m *maskWriter) Write(p []byte) (int, error) {
	m.pending = append(m.pending, p...)
	for _, s := range m.secrets {
		m.pending = bytes.ReplaceAll(m.pending, s, maskTok)
	}
	out := m.pending[:len(m.pending)-holdLen(m.pending, m.secrets)]
	if _, err := m.dst.Write(out); err != nil {
		return 0, err
	}
	m.pending = append([]byte(nil), m.pending[len(out):]...)
	return len(p), nil
}

func (m *maskWriter) Flush() error {
	if len(m.pending) == 0 {
		return nil
	}
	_, err := m.dst.Write(m.pending)
	m.pending = nil
	return err
}

// Maior prefixo do fim do buffer que ainda pode completar um segredo
func holdLen(buf []byte, secrets [][]byte) int {
	hold := 0
	for _, s := range secrets {
		for k := min(len(s)-1, len(buf)); k > hold; k-- {
			if bytes.HasSuffix(buf, s[:k]) {
				hold = k
				break
			}
		}
	}
	return hold
}
