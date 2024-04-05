package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"fmt"
	"runtime/debug"
	"path"
	lsp "go.lsp.dev/protocol"
	"go.lsp.dev/uri"
	"github.com/zyedidia/micro/v2/internal/config"
	"github.com/zyedidia/tcell/v2"
	"github.com/zyedidia/micro/v2/internal/screen"
)

type STATE int

type Diagnostic struct {
	lsp.Diagnostic
	Server *Server
}

const (
	STATE_CREATED STATE = iota
	STATE_INITIALIZED
	STATE_RUNNING
	STATE_RESTARTING
)

func (s STATE) String() string {
	switch s {
		case STATE_CREATED: return "created"
		case STATE_INITIALIZED: return "initialized"
		case STATE_RUNNING: return "running"
		case STATE_RESTARTING: return "restarting"
	}
	return "unknown(" + fmt.Sprint(int(s)) + ")"
}

var servers map[string]*Server
var slock sync.Mutex

func init() {
	servers = make(map[string]*Server)
}

func getServer(l LSPConfig, dir string) *Server {
	s, ok := servers[l.Name+"-"+dir]
	if !ok { return nil }
	return s
}

func GetOrStartServer(l LSPConfig, dir string, path string) *Server {
	if !l.Valid_For(path) { return nil }

	s := getServer(l, dir)
	if s == nil {
		var err error
		s, err = startServer(l, dir)
		if err == nil {
			s.initialize()
		} else {
			log.Println(dir, l.Name, "failed to start server: ", err)
		}
	} else if s.State == STATE_CREATED {
		s.runCommand()
		s.initialize()
	}

	return s
}

func GetActiveServerNames() []string {
	var activeServers []string

	for _, server := range servers {
		if server.State != STATE_CREATED {
			activeServers = append(activeServers, server.language.Name)
		}
	}

	return activeServers
}

func ShutdownAllServers() {
	for _, s := range servers {
		if s.State != STATE_CREATED {
			s.Shutdown()
		}
	}
}

type Server struct {
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       *bufio.Reader
	language     *LSPConfig
	capabilities lsp.ServerCapabilities
	root         string
	lock         sync.Mutex
	State        STATE
	requestID    int
	responses    map[int]chan ([]byte)
	diagnostics  sync.Map
}

type RPCRequest struct {
	RPCVersion string      `json:"jsonrpc"`
	ID         int         `json:"id"`
	Method     string      `json:"method"`
	Params     interface{} `json:"params"`
}

type RPCNotification struct {
	RPCVersion string      `json:"jsonrpc"`
	Method     string      `json:"method"`
	Params     interface{} `json:"params"`
}

type RPCInit struct {
	RPCVersion string               `json:"jsonrpc"`
	ID         int                  `json:"id"`
	Result     lsp.InitializeResult `json:"result"`
}

type RPCResult struct {
	RPCVersion string `json:"jsonrpc"`
	ID         int    `json:"id,omitempty"`
	Method     string `json:"method,omitempty"`
}

type RPCDiag struct {
	RPCVersion string `json:"jsonrpc"`
	ID     int                          `json:"id,omitempty"`
	Method string                       `json:"method,omitempty"`
	Params lsp.PublishDiagnosticsParams `json:"params"`
}


func env_to_strs(env map[string]string) []string {
	var out []string
	for key, val := range env {
		out = append(out, key + "=" + val)
	}
	return out
}

func (s *Server) state_guard(states ...STATE) error {
	for _, state := range states {
		if s.State == state { return nil }
	}

	states_string := ""
	last := len(states)-1
	for i, state := range states {
		if i != 0 && i != last {
			states_string += ", "
		} else if i != 0 && i == last {
			states_string += " or "
		}

		states_string += state.String()
	}

	return errors.New("Expected state to be " + states_string + ", but " + s.language.Name + " is " + s.State.String())
}

func (s *Server) runCommand() error {
	if err := s.state_guard(STATE_CREATED) ; err != nil { return err }
	if s.cmd != nil { return errors.New(s.language.Name + " is already running.") }

	cmd, err := s.language.GetCmd(s.root)
	if err != nil { return err }
	c := exec.Command(cmd.tokens[0], cmd.tokens[1:]...)

	var env = os.Environ()
	add_env, err := s.language.GetEnv()
	if err != nil { return err }

	c.Env = append(env, env_to_strs(add_env)...)
	c.Dir = s.root

	c.Stderr = log.Writer()

	stdin, err := c.StdinPipe()
	if err != nil {
		s.Log(err)
		return err
	}

	stdout, err := c.StdoutPipe()
	if err != nil {
		s.Log(err)
		return err
	}

	err = c.Start()
	if err != nil {
		s.Log(err)
		return err
	}

	s.cmd = c
	s.stdin = stdin
	s.stdout = bufio.NewReader(stdout)

	return nil
}

func startServer(l LSPConfig, dir string) (*Server, error) {
	s := new(Server)

	cwd, err := l.GetCwd()
	if err != nil { return nil, err }
	if len(cwd) == 0 { cwd = dir }

	s.root = cwd
	s.language = &l
	s.responses = make(map[int]chan []byte)

	err = s.runCommand()
	if err != nil { return nil, err }
	s.State = STATE_INITIALIZED

	return s, nil
}

func (s *Server) Log(args ...any) {
	tp := []any{"[lsp: "+s.GetLanguage().Name+"]"}
	tp = append(tp, args...)
	log.Println(tp...)
}

// initialize performs the LSP initialization handshake
// The directory must be an absolute path
func (s *Server) initialize() {
	var options any = s.language.Options

	config_path := path.Join(s.root, s.language.Name + ".mlsp.json")
	if _, err := os.Stat(config_path); !errors.Is(err, os.ErrNotExist) {
		data, err := os.ReadFile(config_path)
		if err == nil {
			var new_options any = make(map[string]any)
			err := json.Unmarshal(data, &new_options)
			if err == nil {
				options = new_options
			} else {
				s.Log("Failed to parse config at", config_path)
			}
		} else {
			s.Log("Failed to read config at", config_path)
		}
	} else {
		s.Log(config_path, "does not exist, using default options.")
	}

	params := lsp.InitializeParams{
		ProcessID: int32(os.Getpid()),
		RootURI:   uri.File(s.root),
		WorkspaceFolders: []lsp.WorkspaceFolder{
			{ Name: path.Base(s.root), URI: string(uri.File(s.root)) },
		},
		InitializationOptions: options,
		Capabilities: lsp.ClientCapabilities{
			Workspace: &lsp.WorkspaceClientCapabilities{
				WorkspaceEdit: &lsp.WorkspaceClientCapabilitiesWorkspaceEdit{
					DocumentChanges:    true,
					ResourceOperations: []string{"create", "rename", "delete"},
				},
				ApplyEdit: true,
			},
			TextDocument: &lsp.TextDocumentClientCapabilities{
				Formatting: &lsp.DocumentFormattingClientCapabilities{
					DynamicRegistration: true,
				},
				Completion: &lsp.CompletionTextDocumentClientCapabilities{
					DynamicRegistration: true,
					CompletionItem: &lsp.CompletionTextDocumentClientCapabilitiesItem{
						SnippetSupport:          false,
						CommitCharactersSupport: false,
						DocumentationFormat:     []lsp.MarkupKind{lsp.PlainText},
						DeprecatedSupport:       false,
						PreselectSupport:        false,
						InsertReplaceSupport:    false,
					},
					ContextSupport: false,
				},
				Rename: &lsp.RenameClientCapabilities{
					DynamicRegistration: true,
					PrepareSupport: true,
					HonorsChangeAnnotations: false,
				},
				Hover: &lsp.HoverTextDocumentClientCapabilities{
					DynamicRegistration: true,
					ContentFormat:       []lsp.MarkupKind{lsp.PlainText},
				},
			},
		},
	}

	servers[s.language.Name+"-"+s.root] = s
	s.State = STATE_RUNNING

	go s.receive()

	s.lock.Lock()
	go func() {
		resp, err := s.sendRequest(lsp.MethodInitialize, params)
		if err != nil {
			s.Log(err)
			s.Murder()
			s.lock.Unlock()
			return
		}

		// todo parse capabilities
		s.Log("<<<", string(resp))

		var r RPCInit
		json.Unmarshal(resp, &r)

		s.lock.Unlock()
		err = s.sendNotification(lsp.MethodInitialized, struct{}{})
		if err != nil { s.Log(err) }

		s.capabilities = r.Result.Capabilities
	}()
}

func (s *Server) GetLanguage() *LSPConfig {
	return s.language
}

func (s *Server) GetCommand() *exec.Cmd {
	return s.cmd
}

func (s *Server) Shutdown() {
	if s.state_guard(STATE_INITIALIZED, STATE_RUNNING) != nil { return }
	s.sendRequest(lsp.MethodShutdown, nil)
	s.sendNotification(lsp.MethodExit, nil)
	s.Murder()
}

func (s *Server) Murder() {
	defer func() {
		if err := recover(); err != nil {
			str := string(debug.Stack())
			log.Println("panic occurred:", err)
			log.Println(str)
		}
	}()

	s.State = STATE_CREATED
	if s.cmd.ProcessState.ExitCode() == -1 {
		s.cmd.Process.Kill()
	}
	s.cmd = nil
}

func (s *Server) Restart() {
	if s.state_guard(STATE_INITIALIZED, STATE_RUNNING) != nil { return }
	s.State = STATE_RESTARTING
	s.sendRequest(lsp.MethodShutdown, nil)
	s.sendNotification(lsp.MethodExit, nil)
	s.Murder()
	s.runCommand()
	s.initialize()
}

func convertDiagnostics(s *Server, diags []lsp.Diagnostic) []Diagnostic {
	out := make([]Diagnostic, len(diags))
	for i, diag := range diags {
		out[i].Diagnostic = diag
		out[i].Server = s
	}
	return out
}

func (s *Server) storeDiagnostics(uri uri.URI, diag []Diagnostic) {
	s.diagnostics.Store(uri, diag)
}

func (s *Server) loadDiagnostics(uri uri.URI) []Diagnostic {
	diags, ok := s.diagnostics.Load(uri)
	if !ok { return nil }
	return diags.([]Diagnostic)
}

func (s *Server) receive() {
	for s.State != STATE_CREATED {
		resp, err := s.receiveMessage()
		if err == io.EOF {
			s.Log("Received EOF, shutting down")
			s.Murder()
			return
		}
		if err != nil {
			s.Log(err)
			continue
		}

		var r RPCResult
		err = json.Unmarshal(resp, &r)
		if err != nil {
			s.Log(err)
			continue
		}

		switch r.Method {
		case lsp.MethodWindowLogMessage:
			// TODO
		case lsp.MethodClientRegisterCapability:
		case lsp.MethodClientUnregisterCapability:
		case lsp.MethodTextDocumentPublishDiagnostics:
			var diag RPCDiag
			err = json.Unmarshal(resp, &diag)
			if err != nil {
				s.Log(err)
				continue
			}
			fileuri := uri.URI(string(diag.Params.URI))
			s.storeDiagnostics(fileuri, convertDiagnostics(s, diag.Params.Diagnostics))
		case "":
			// Response
			if _, ok := s.responses[r.ID]; ok {
				s.Log("Got response for", r.ID)
				s.responses[r.ID] <- resp
			}
		}
	}
}

func Style(d *Diagnostic) tcell.Style {
	switch d.Severity {
	case lsp.DiagnosticSeverityInformation:
	case lsp.DiagnosticSeverityHint:
		if style, ok := config.Colorscheme["gutter-info"]; ok {
			return style
		}
	case lsp.DiagnosticSeverityWarning:
		if style, ok := config.Colorscheme["gutter-warning"]; ok {
			return style
		}
	case lsp.DiagnosticSeverityError:
		if style, ok := config.Colorscheme["gutter-error"]; ok {
			return style
		}
	}
	return config.DefStyle
}

func (s *Server) GetDiagnostics(filename string) []Diagnostic {
	fileuri := uri.File(filename)
	return s.loadDiagnostics(fileuri)
}

func (s *Server) DiagnosticsCount(filename string) int {
	fileuri := uri.File(filename)
	diags := s.loadDiagnostics(fileuri)
	if diags == nil { return 0 }
	return len(diags)
}

func (s *Server) receiveMessage() (outbyte []byte, err error) {
	defer func() {
		if r:= recover(); r != nil {
			s.Log("Receive error:", r)
			err = fmt.Errorf("pkg: %v", r)
			outbyte = nil
		} else {
			go screen.Redraw();
		}
	}()

	n := -1
	for {
		b, err := s.stdout.ReadBytes('\n')
		if err != nil { s.Log(err) ; return nil, err }

		headerline := strings.TrimSpace(string(b))
		if len(headerline) == 0 { break }

		if strings.HasPrefix(headerline, "Content-Length:") {
			split := strings.Split(headerline, ":")
			if len(split) <= 1 { break }
			n, err = strconv.Atoi(strings.TrimSpace(split[1]))
			if err != nil { s.Log(err) ; return nil, err }
		}
	}

	if n <= 0 {
		return []byte{}, nil
	}

	outbyte = make([]byte, n)
	_, err = io.ReadFull(s.stdout, outbyte)
	if err != nil { s.Log(err) }
	return outbyte, err
}

func (s *Server) sendNotification(method string, params interface{}) error {
	if err := s.state_guard(STATE_INITIALIZED, STATE_RUNNING, STATE_RESTARTING) ; err != nil {
		return err
	}

	m := RPCNotification{
		RPCVersion: "2.0",
		Method:     method,
		Params:     params,
	}

	s.lock.Lock()
	go s.sendMessageUnlock(m)
	return nil
}

func (s *Server) sendRequest(method string, params interface{}) ([]byte, error) {
	if err := s.state_guard(STATE_INITIALIZED, STATE_RUNNING, STATE_RESTARTING) ; err != nil {
		return nil, err
	}

	id := s.requestID
	s.requestID++
	r := make(chan []byte)
	s.responses[id] = r

	m := RPCRequest{
		RPCVersion: "2.0",
		ID:         id,
		Method:     method,
		Params:     params,
	}

	err := s.sendMessage(m)
	if err != nil {
		s.Log(err)
		return nil, err
	}

	var bytes []byte
	select {
	case bytes = <-r:
	case <-time.After(5 * time.Second):
		err = errors.New("Request timed out")
	}
	delete(s.responses, id)

	if err != nil { s.Log(err) }

	return bytes, err
}

func (s *Server) sendMessage(m interface{}) error {
	msg, err := json.Marshal(m)
	if err != nil {
		return err
	}

	strmsg := string(msg)
	if !strings.Contains(strmsg, `"method":"textDocument/didOpen"`) {
		s.Log(">>>", string(msg))
	} else {
		s.Log(">>> textDocument/didOpen (truncated)")
	}

	// encode header and proper line endings
	msg = append(msg, '\r', '\n')
	header := []byte("Content-Length: " + strconv.Itoa(len(msg)) + "\r\n\r\n")
	msg = append(header, msg...)

	_, err = s.stdin.Write(msg)
	return err
}

func (s *Server) sendMessageUnlock(m interface{}) error {
	defer s.lock.Unlock()
	return s.sendMessage(m)
}
