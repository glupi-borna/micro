package lsp

import (
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"log"
	"reflect"
	"fmt"
	"runtime/debug"

	"github.com/zyedidia/micro/v2/internal/config"
	"gopkg.in/yaml.v2"
	lua "github.com/yuin/gopher-lua"
	ulua "github.com/zyedidia/micro/v2/internal/lua"
	luar "layeh.com/gopher-luar"
)

var ErrManualInstall = errors.New("Requires manual installation")
var ErrUnknownInstall = errors.New("Unknown installation method")

type ConfigStatic struct {
	Languages map[string]LanguageStatic `yaml:"language"`
}

type Config struct {
	Languages map[string]Language
}

type LanguageStatic struct {
	Name        string
	Command     string 				`yaml:"command"`
	Args        []string            `yaml:"args"`
	IsInstalled []string			`yaml:"is_installed"`
	Install     [][]string			`yaml:"install"`
	Env         map[string]string 	`yaml:"env"`
	Cwd         string 				`yaml:"cwd"`
}

type Language struct {
	Name		string
	Command		Runnable
	IsInstalled	Runnable
	Install		Runnable
	Env			Runnable
	Cwd			Runnable
}

type Runnable interface {
	Run(Language, ...any) (any, error)
}

type Command struct {
	tokens []string
}

func (cmd *Command) Run(l Language, args ...any) (any, error) {
	log.Println(strings.Join(cmd.tokens, " ")+"\n")
	var cmdr *exec.Cmd
	if len(cmd.tokens) > 1 {
		cmdr = exec.Command(cmd.tokens[0], cmd.tokens[1:]...)
	} else if len(cmd.tokens) == 0 {
		return nil, errors.New(fmt.Sprint("Command can not be empty!"))
	} else {
		cmdr = exec.Command(cmd.tokens[0])
	}
	err := cmdr.Run()
	return nil, err
}

type Commands struct {
	cmds []Command
}

func MakeCommands(arr [][]string) *Commands {
	var cmds []Command
	for _, tokens := range arr {
		cmds = append(cmds, Command{tokens})
	}
	return &Commands{cmds}
}

func (cmds *Commands) Run(l Language, args ...any) (any, error) {
	var vals []any
	for _, cmd := range cmds.cmds {
		val, err := cmd.Run(l)
		if err != nil { return nil, err }
		vals = append(vals, val)
	}
	return vals, nil
}

type LUAFn struct {
	fn lua.LValue
}

func lua_args(args ...any) []lua.LValue {
	var out []lua.LValue
	for _, arg := range args {
		out = append(out, luar.New(ulua.L, arg))
	}
	return out
}

func (lf *LUAFn) Run(l Language, args ...any) (any, error) {
	var largs []lua.LValue
	largs = append(largs, luar.New(ulua.L, l))
	largs = append(largs, lua_args(args)...)
	return call(lf.fn, largs...)
}

type Fn struct {
	fn func (...any) []any
}

func (lf *Fn) Run(l Language, args ...any) (any, error) {
	var fnargs []any
	fnargs = append(fnargs, l)
	fnargs = append(fnargs, args...)
	return lf.fn(fnargs...), nil
}

type Env struct {
	dict map[string]string
}

func (env *Env) Run(l Language, args ...any) (any, error) {
	return env.dict, nil
}

type Str struct {
	str string
}

func (str *Str) Run(l Language, args ...any) (any, error) {
	return str.str, nil
}

type NoOp struct {}
func (*NoOp) Run(l Language, args ...any) (any, error) { return nil, ErrManualInstall }

type ResolutionContext struct {
	l Language
	from any
	errname string
}

func (ctx ResolutionContext) modified(from any, errappend string) ResolutionContext {
	return ResolutionContext{ ctx.l, from, ctx.errname + errappend }
}

func (ctx ResolutionContext) Error(msg string) error {
	if len(msg) > 0 {
		return errors.New("Error parsing '" + ctx.errname + "' for LSP " + ctx.l.Name + ": " + msg)
	} else {
		return errors.New("Error parsing '" + ctx.errname + "' for LSP " + ctx.l.Name)
	}
}

var conf *Config

func GetLanguage(lang string) (Language, bool) {
	if conf != nil {
		log.Println("Getting server for ", lang)
		l, ok := conf.Languages[lang]
		return l, ok
	}
	return Language{}, false
}

func Init() error {
	var servers []byte
	var err error
	filename := filepath.Join(config.ConfigDir, "lsp.yaml")
	if _, e := os.Stat(filename); e == nil {
		servers, err = ioutil.ReadFile(filename)
		if err != nil {
			servers = servers_internal
		}
	} else {
		err = ioutil.WriteFile(filename, servers_internal, 0644)
		servers = servers_internal
	}

	conf, err = LoadConfig(servers)
	if err != nil { return err }

	for k, v := range conf.Languages {
		if v.Name == "" { v.Name = k }
	}
	return nil
}

func castArray[K any](ctx ResolutionContext, arr any) []K {
	var out []K
	if arr == nil { return out }
	val := reflect.ValueOf(arr)
	n := val.Len()
	for i := 0; i<n; i++ {
		v, ok := val.Index(i).Interface().(K)
		if !ok {
			ktype := reflect.TypeOf(v)
			vtype := reflect.TypeOf(val.Index(i).Interface())
			panic(fmt.Sprint("Failed to convert value of type ", vtype, " to ", ktype))
		}
		out = append(out, v)
	}
	return out
}

func castArrayDouble[K any](ctx ResolutionContext, arr interface{}) [][]K {
	var out [][]K
	if arr == nil { return out }
	val := reflect.ValueOf(arr)
	n := val.Len()
	for i := 0; i<n; i++ {
		subarr := castArray[K](ctx, val.Index(i).Interface())
		out = append(out, subarr)
	}
	return out
}

func castMap[K comparable, V any](ctx ResolutionContext, m interface{}) map[K]V {
	out := make(map[K]V)
	if m == nil { return out }
	val := reflect.ValueOf(m)
	keys := val.MapKeys()
	for _, k := range keys {
		out[k.Interface().(K)] = val.MapIndex(k).Interface().(V)
	}
	return out
}

func castValue[K any](ctx ResolutionContext, val any) K {
	resolved, ok := val.(K)
	if ok { return resolved }
	errtext := expected[K](val)
	panic("Resolver failed for " + ctx.errname + " for LSP " + ctx.l.Name + ": " + errtext)
}

func expected[EXPECTED any](val any) string {
	var e EXPECTED
	et := reflect.TypeOf(e)
	gt := reflect.TypeOf(val)
	return fmt.Sprint("Expected ", et, ", got ", gt, "(", val, ")")
}

func MakeRunnable(l Language, propname string, val any, strict bool) Runnable {
	ctx := ResolutionContext{ l, val, propname }

	if val == nil && !strict { return &NoOp{} }

	strarrarr, err := lspResolveArray(ctx, lspArrayResolver(lspResolveString, true), true)
	if err == nil {
		CAST := castArrayDouble[string]
		return MakeCommands( CAST(ctx, strarrarr) )
	}

	strarr, err := lspResolveArray(ctx, lspResolveString, true)
	if err == nil {
		CAST := castArray[string]
		return &Command{ CAST(ctx, strarr) }
	}

	str, err := lspResolveString(ctx)
	if err == nil {
		CAST := castValue[string]
		return &Str{ CAST(ctx, str) }
	}

	fn, err := lspResolveFunction(ctx)
	if err == nil {
		CAST := castValue[func(...any)[]any]
		return &Fn{ CAST(ctx, fn) }
	}

	lfn, err := lspResolveLuaFunction(ctx)
	if err == nil {
		CAST := castValue[lua.LValue]
		return &LUAFn{ CAST(ctx, lfn) }
	}

	dict, err := lspResolveMap(ctx, lspResolveString)
	if err == nil {
		CAST := castMap[string, string]
		return &Env{ CAST(ctx, dict) }
	}

	if strict {
		errtxt := fmt.Sprint("All resolvers failed for ", ctx.errname, " for LSP ", ctx.l.Name, " ", val)
		log.Println(errtxt)
		panic(errtxt)
	}

	return &NoOp{}
}

func RegisterLanguageServer(
	language string,
	name string,
	cmd any,
	install any,
	is_installed any,
	env any,
	cwd any,
) {
	var l Language
	l.Name = name

	l.Command = MakeRunnable(l, "Command", cmd, true)
	l.Install = MakeRunnable(l, "Install", install, false)
	l.IsInstalled = MakeRunnable(l, "IsInstalled", is_installed, false)
	l.Env = MakeRunnable(l, "Env", env, false)
	l.Cwd = MakeRunnable(l, "Cwd", cwd, false)

	log.Println("Registering language server: ", l)

	conf.Languages[language] = l
}

type Resolver func(ResolutionContext) (any, error)

func lspResolveAny(
	ctx ResolutionContext,
	resolvers ...Resolver,
) (any, error) {
	for _, resolver := range resolvers {
		val, err := resolver(ctx)
		if err == nil { return val, nil }
	}
	return nil, ctx.Error("")
}

func lspAnyResolver(resolvers ...Resolver) Resolver {
	return func(ctx ResolutionContext) (any, error) {
		return lspResolveAny(ctx, resolvers...)
	}
}

func lspResolveArray(
	ctx ResolutionContext,
	resolve_item Resolver,
	nonempty bool,
) (any, error) {
	t := reflect.TypeOf(ctx.from)
	if t != nil && t.Kind() == reflect.Slice {
		slice := reflect.ValueOf(ctx.from)
		var out_arr []any
		l := slice.Len()
		if l == 0 && nonempty { return nil, ctx.Error("Array must have at least 1 element!") }
		for i := 0 ; i < l; i++ {
			val := slice.Index(i).Interface()
			item, err := resolve_item(ctx.modified(val, "[" + fmt.Sprint(i) + "]"))
			if err != nil { return nil, err }
			out_arr = append(out_arr, item)
		}
		return out_arr, nil
	}

	lua_table, ok := ctx.from.(*lua.LTable)
	if ok {
		var out_arr []any
		l := lua_table.MaxN()
		if l == 0 && nonempty { return nil, ctx.Error("Array must have at least 1 element!") }
		for i := 0 ; i<=l ; i++ {
			item, err := resolve_item(ctx.modified(lua_table.RawGetInt(i), "[" + fmt.Sprint(i) + "]"))
			if err != nil { return nil, err }
			out_arr = append(out_arr, item)
		}
		return out_arr, nil
	}

	return nil, ctx.Error("Expected an array")
}

func lspArrayResolver(resolve_item Resolver, nonempty bool) Resolver {
	return func (ctx ResolutionContext) (any, error) {
		return lspResolveArray(ctx, resolve_item, nonempty)
	}
}

func lspResolveMap(
	ctx ResolutionContext,
	resolve_value Resolver,
) (any, error) {
	t := reflect.TypeOf(ctx.from)
	if t != nil && t.Kind() == reflect.Map {
		dict := reflect.ValueOf(ctx.from)
		var out_map map[string]any
		keys := dict.MapKeys()
		for _, key := range keys {
			if key.Type().Kind() != reflect.String {
				return nil, ctx.Error("Expected keys to be of type string")
			}
			val := dict.MapIndex(key).Interface()
			item, err := resolve_value(ctx.modified(val, "[" + key.String() + "]"))
			if err != nil { return nil, err }
			out_map[key.String()] = item
		}
		return out_map, nil
	}

	lua_table, ok := ctx.from.(*lua.LTable)
	if ok {
		var out_map map[string]any
		var err error
		lua_table.ForEach(func (key lua.LValue, val lua.LValue) {
			if err != nil { return }
			if key.Type() == lua.LTString {
				var item any
				item, err = resolve_value(ctx.modified(val, ""))
				if err != nil { return }
				out_map[key.String()] = item
			}
		})

		if err != nil { return nil, err }
		return out_map, nil
	}

	return nil, ctx.Error("Expected a key-value map")
}

func lspMapResolver(resolve_value Resolver) Resolver {
	return func(ctx ResolutionContext) (any, error) {
		return lspResolveMap(ctx, resolve_value)
	}
}

func lspResolveString(ctx ResolutionContext) (any, error) {
	switch val := ctx.from.(type) {
	case string: return val, nil
	case lua.LValue:
		if val.Type() == lua.LTString {
			str := lua.LVAsString(val)
			return str, nil
		}
	}
	return "", ctx.Error("Expected a string")
}

func lspResolveFunction(ctx ResolutionContext) (any, error) {
	switch val := ctx.from.(type) {
	case func(...any) []any:
		return val, nil
	default:
		return nil, ctx.Error("Expected a function")
	}
}

func lspResolveLuaFunction(ctx ResolutionContext) (any, error) {
	switch val := ctx.from.(type) {
	case lua.LValue, *lua.LValue, lua.LFunction, *lua.LFunction:
		return val, nil
	default:
		return nil, ctx.Error("Expected a Lua function")
	}
}

func luaGet[K any](l Language, luafn *LUAFn, resolver Resolver, propname string, args ...any) (K, error) {
	var empty K
	val, err := luafn.Run(l, args...)
	if err != nil { return empty, err }
	ctx := ResolutionContext{l, val, propname}
	resolved, err := resolver(ctx)
	if err != nil { return empty, err }
	return castValue[K](ctx.modified(ctx.from, ":LUAGET:"), resolved), nil
}

func (l Language) GetCmd(root string) (*Command, error) {
	switch cmd := l.Command.(type) {
	case *Command:
		log.Println("CMD is a cmd")
		return cmd, nil
	case *Str:
		log.Println("CMD is a str")
		return &Command{[]string{cmd.str}}, nil
	case *LUAFn:
		log.Println("CMD is a lfn")
		resolver := lspArrayResolver(lspResolveString, true)
		getter := luaGet[[]string]
		val, err := getter(l, cmd, resolver, "Command", root)
		if err != nil { return nil, err }
		return &Command{ val }, nil
	case *Fn:
		log.Println("CMD is a fn")
		resolver := lspArrayResolver(lspResolveString, true)
		val, err := cmd.Run(l, root)
		if err != nil { return nil, err }
		ctx := ResolutionContext{l, val, "Command"}
		val, err = resolver(ctx)
		if err != nil { return nil, err }
		strarr := castArray[string](ctx, val)
		return &Command{ strarr }, nil
	}

	return nil, errors.New("Failed to get Command for LSP " + l.Name + " " + expected[Command](l.Command))
}

func (l Language) GetInstall() (*Commands, error) {
	switch cmds := l.Install.(type) {
	case *Str: return MakeCommands([][]string{{cmds.str}}), nil
	case *Command: return &Commands{[]Command{*cmds}}, nil
	case *Commands: return cmds, nil
	case *LUAFn:
		resolver := lspArrayResolver(lspArrayResolver(lspResolveString, true), true)
		getter := luaGet[[][]string]
		val, err := getter(l, cmds, resolver, "Install")
		if err != nil { return nil, err }
		return MakeCommands(val), nil
	}
	return nil, errors.New("Failed to get Install for LSP " + l.Name + " " + expected[Commands](l.Install))
}

func (l Language) GetIsInstalled() (Runnable, error) {
	switch cmd := l.IsInstalled.(type) {
	case *Str: return &Command{[]string{cmd.str}}, nil
	case *Command: return cmd, nil
	case *LUAFn: return cmd,  nil
	case *Fn: return cmd, nil
	case *NoOp: return cmd, nil
	default: return nil, errors.New(expected[Command](cmd))
	}
}

func (l Language) GetEnv() (map[string]string, error) {
	switch env := l.Env.(type) {
	case *Env: return env.dict, nil
	case *LUAFn:
		resolver := lspMapResolver(lspResolveString)
		getter := luaGet[map[string]string]
		val, err := getter(l, env, resolver, "Env")
		if err != nil { return nil, err }
		return val, nil
	case *Fn:
		resolver := lspMapResolver(lspResolveString)
		val, err := env.Run(l)
		if err != nil { return nil, err }
		ctx := ResolutionContext{l, val, "Env"}
		val, err = resolver(ctx)
		if err != nil { return nil, err }
		m := castMap[string, string](ctx, val)
		return m, nil
	case *NoOp: return nil, nil
	}
	return nil, errors.New("Failed to get Env for LSP " + l.Name + " " + expected[Env](l.Env))
}

func (l Language) GetCwd() (string, error) {
	switch cwd := l.Cwd.(type) {
		case *Str: return cwd.str, nil
		case *LUAFn:
			getter := luaGet[string]
			val, err := getter(l, cwd, lspResolveString, "Cwd")
			if err != nil { return "nil", err }
			return val, nil
		case *Fn:
			val, err := cwd.Run(l)
			if err != nil { return "", err }
			ctx := ResolutionContext{l, val, "Cwd"}
			val, err = lspResolveString(ctx)
			if err != nil { return "", err }
			return castValue[string](ctx, val), nil
		case *NoOp: return "", nil
	}
	return "", errors.New("Failed to get Cwd for LSP " + l.Name + " " + expected[string](l.Cwd))
}

func RunnableString(r Runnable) string {
	switch v := r.(type) {
		case *Command:
			return "Command{" + strings.Join(v.tokens, " ") + "}"
		case *Commands:
			out := "Commands{"
			for _, cmd := range v.cmds {
				out += "\t" + RunnableString(&cmd) + "\n"
			}
			return out + "\n}"
		case *NoOp: return "NoOp{}"
		case *Str: return "Str{}"
		case *Env: return "Env{}"
		default: return "Unknown"
	}
}

func LoadConfig(data []byte) (*Config, error) {
	defer func() {
		if err := recover(); err != nil {
			str := string(debug.Stack())
			log.Println("panic occurred:", err)
			log.Println(str)
		}
	}()

	var sconf ConfigStatic
	if err := yaml.Unmarshal(data, &sconf); err != nil {
		return nil, err
	}

	var conf Config
	conf.Languages = make(map[string]Language)

	for key, lang := range sconf.Languages {
		var l Language
		if len(lang.Name) > 0 {
			l.Name = lang.Name
		} else if len(lang.Command) > 0 {
			l.Name = lang.Command
		} else {
			l.Name = key
		}
		var cmd []string
		cmd = append(cmd, lang.Command)
		cmd = append(cmd, lang.Args...)
		l.Command = MakeRunnable(l, "Command", cmd, true)
		l.Cwd = MakeRunnable(l, "Cwd", lang.Cwd, false)
		l.Env = MakeRunnable(l, "Env", lang.Env, false)
		l.Install = MakeRunnable(l, "Install", lang.Install, false)
		l.IsInstalled = MakeRunnable(l, "IsInstall", lang.IsInstalled, false)
		conf.Languages[key] = l
	}

	return &conf, nil
}

func call(fn lua.LValue, args ...lua.LValue) (lua.LValue, error) {
	if fn == lua.LNil { return nil, config.ErrNoSuchFunction }
	err := ulua.L.CallByParam(lua.P{
		Fn:      fn,
		NRet:    1,
		Protect: true,
	}, args...)
	if err != nil { return nil, err }
	ret := ulua.L.Get(-1)
	ulua.L.Pop(1)
	return ret, nil
}

func (l Language) Installed() bool {
	is_installed, err := l.GetIsInstalled()
	if err != nil {
		log.Println(l.Name, "IsInstalled error (get):", err);
		return false
	}

	_, is_noop := is_installed.(*NoOp)
	if is_noop {
		cmd, err := l.GetCmd("")
		if err != nil {
			log.Println(l.Name, is_installed, "IsInstalled error (noop):", err)
			return false
		}
		if len(cmd.tokens) == 0 { return false }
		_, err = exec.LookPath(cmd.tokens[0])
		if err != nil {
			log.Println(l.Name, "IsInstalled error (noop):", err);
			return false
		}
		return true
	}

	ok, err := is_installed.Run(l)
	if err != nil {
		log.Println(l.Name, "IsInstalled error:", err)
		return false
	}

	if ok == nil {
		log.Println(l.Name, "IsInstalled returns nil.")
		return false
	}

	okarr, ok_is_arr := ok.([]interface{})
	if ok_is_arr && len(okarr) > 0 { ok = okarr[0] }

	switch val := ok.(type) {
		case bool: return val
		case lua.LValue: return lua.LVAsBool(val)
		case lua.LBool: return lua.LVAsBool(val)
		default: log.Println(l.Name, "Warning: IsInstalled returns incorrect type! Got: ", reflect.TypeOf(val), val, RunnableString(is_installed))
	}
	return false
}

func (l Language) DoInstall() error {
	if l.Installed() { return nil }
	cmds, err := l.GetInstall()
	if err != nil { return err }
	_, err = cmds.Run(l)
	return err
}
