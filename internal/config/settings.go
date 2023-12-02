package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"log"

	"github.com/zyedidia/glob"
	"github.com/zyedidia/json5"
	"github.com/zyedidia/micro/v2/internal/util"
	"golang.org/x/text/encoding/htmlindex"
)

type optionValidator func(string, interface{}) error

var (
	ErrInvalidOption = errors.New("Invalid option")
	ErrInvalidValue  = errors.New("Invalid value")

	// The options that the user can set
	GlobalSettings map[string]interface{}

	// This is the raw parsed json
	parsedSettings     map[string]interface{}
	settingsParseError bool

	// ModifiedSettings is a map of settings which should be written to disk
	// because they have been modified by the user in this session
	ModifiedSettings map[string]bool
)

func init() {
	ModifiedSettings = make(map[string]bool)
	parsedSettings = make(map[string]interface{})
}

// Options with validators
var optionValidators = map[string]optionValidator{
	"autosave":     validateGreaterEqual(0),
	"clipboard":    validateStringLiteral("internal", "external", "terminal"),
	"tabsize":      validateGreater(0),
	"scrollmargin": validateGreaterEqual(0),
	"scrollspeed":  validateGreaterEqual(0),
	"colorscheme":  validateCalculatedStringLiteral(GetColorschemeNames),
	"colorcolumn":  validateAny(
		validateArray(validateGreaterEqual(0)),
		validateGreaterEqual(0)),
	"fileformat":   validateStringLiteral("unix", "dos"),
	"encoding":     validateEncoding,
}

func ReadSettings() error {
	filename := filepath.Join(ConfigDir, "settings.json")
	if _, e := os.Stat(filename); e == nil {
		input, err := ioutil.ReadFile(filename)
		if err != nil {
			settingsParseError = true
			return errors.New("Error reading settings.json file: " + err.Error())
		}
		if !strings.HasPrefix(string(input), "null") {
			// Unmarshal the input into the parsed map
			err = json5.Unmarshal(input, &parsedSettings)
			if err != nil {
				settingsParseError = true
				return errors.New("Error reading settings.json: " + err.Error())
			}

			// check if autosave is a boolean and convert it to float if so
			if v, ok := parsedSettings["autosave"]; ok {
				s, ok := v.(bool)
				if ok {
					if s {
						parsedSettings["autosave"] = 8.0
					} else {
						parsedSettings["autosave"] = 0.0
					}
				}
			}
		}
	}
	return nil
}

var interfaceArr []interface{}
var InterfaceArr = reflect.TypeOf(interfaceArr)

func verifySetting(option string, value interface{}, def reflect.Type) bool {
	vtype := reflect.TypeOf(value)

	if option == "pluginrepos" || option == "pluginchannels" {
		return vtype.AssignableTo(InterfaceArr)
	}

	if def.Kind() == reflect.Slice && vtype.Kind() == reflect.Slice {
		varray := value.([]interface{})
		if len(varray) == 0 { return true }
		eltype := reflect.TypeOf(varray[0])
		return eltype.AssignableTo(def.Elem())
	}

	return def.AssignableTo(vtype)
}

// InitGlobalSettings initializes the options map and sets all options to their default values
// Must be called after ReadSettings
func InitGlobalSettings() error {
	var err error
	GlobalSettings = DefaultGlobalSettings()

	for k, v := range parsedSettings {
		if !strings.HasPrefix(reflect.TypeOf(v).String(), "map") {
			if _, ok := GlobalSettings[k]; ok {
				gtype := reflect.TypeOf(GlobalSettings[k])

				if !verifySetting(k, v, gtype) {
					err = fmt.Errorf(
						"Global Error: setting '%s' (%v) has incorrect type (%s), using default value: %v (%s)",
						k, v,
						reflect.TypeOf(v),
						GlobalSettings[k], gtype)
					continue
				}
			}

			GlobalSettings[k] = v
		}
	}
	return err
}

// InitLocalSettings scans the json in settings.json and sets the options locally based
// on whether the filetype or path matches ft or glob local settings
// Must be called after ReadSettings
func InitLocalSettings(settings map[string]interface{}, path string) error {
	var parseError error
	for k, v := range parsedSettings {
		if strings.HasPrefix(reflect.TypeOf(v).String(), "map") {
			if strings.HasPrefix(k, "ft:") {
				if settings["filetype"].(string) == k[3:] {
					for k1, v1 := range v.(map[string]interface{}) {
						if _, ok := settings[k1]; ok && !verifySetting(k1, v1, reflect.TypeOf(settings[k1])) {
							parseError = fmt.Errorf("Error: setting '%s' has incorrect type (%s), using default value: %v (%s)", k, reflect.TypeOf(v1), settings[k1], reflect.TypeOf(settings[k1]))
							continue
						}
						settings[k1] = v1
					}
				}
			} else {
				g, err := glob.Compile(k)
				if err != nil {
					parseError = errors.New("Error with glob setting " + k + ": " + err.Error())
					continue
				}

				if g.MatchString(path) {
					for k1, v1 := range v.(map[string]interface{}) {
						if _, ok := settings[k1]; ok && !verifySetting(k1, v1, reflect.TypeOf(settings[k1])) {
							parseError = fmt.Errorf("Error: setting '%s' has incorrect type (%s), using default value: %v (%s)", k, reflect.TypeOf(v1), settings[k1], reflect.TypeOf(settings[k1]))
							continue
						}
						settings[k1] = v1
					}
				}
			}
		}
	}
	return parseError
}

// WriteSettings writes the settings to the specified filename as JSON
func WriteSettings(filename string) error {
	if settingsParseError {
		// Don't write settings if there was a parse error
		// because this will delete the settings.json if it
		// is invalid. Instead we should allow the user to fix
		// it manually.
		return nil
	}

	var err error
	if _, e := os.Stat(ConfigDir); e == nil {
		defaults := DefaultGlobalSettings()

		// remove any options froms parsedSettings that have since been marked as default
		for k, v := range parsedSettings {
			if !strings.HasPrefix(reflect.TypeOf(v).String(), "map") {
				cur, okcur := GlobalSettings[k]
				if def, ok := defaults[k]; ok && okcur && reflect.DeepEqual(cur, def) {
					delete(parsedSettings, k)
				}
			}
		}

		// add any options to parsedSettings that have since been marked as non-default
		for k, v := range GlobalSettings {
			if def, ok := defaults[k]; !ok || !reflect.DeepEqual(v, def) {
				if _, wr := ModifiedSettings[k]; wr {
					parsedSettings[k] = v
				}
			}
		}

		txt, _ := json.MarshalIndent(parsedSettings, "", "    ")
		err = ioutil.WriteFile(filename, append(txt, '\n'), 0644)
	}
	return err
}

// OverwriteSettings writes the current settings to settings.json and
// resets any user configuration of local settings present in settings.json
func OverwriteSettings(filename string) error {
	settings := make(map[string]interface{})

	var err error
	if _, e := os.Stat(ConfigDir); e == nil {
		defaults := DefaultGlobalSettings()
		for k, v := range GlobalSettings {
			if def, ok := defaults[k]; !ok || !reflect.DeepEqual(v, def) {
				if _, wr := ModifiedSettings[k]; wr {
					settings[k] = v
				}
			}
		}

		txt, _ := json.MarshalIndent(settings, "", "    ")
		err = ioutil.WriteFile(filename, append(txt, '\n'), 0644)
	}
	return err
}

// RegisterCommonOptionPlug creates a new option (called pl.name). This is meant to be called by plugins to add options.
func RegisterCommonOptionPlug(pl string, name string, defaultvalue interface{}) error {
	name = pl + "." + name
	if _, ok := GlobalSettings[name]; !ok {
		defaultCommonSettings[name] = defaultvalue
		GlobalSettings[name] = defaultvalue
		err := WriteSettings(filepath.Join(ConfigDir, "settings.json"))
		if err != nil {
			return errors.New("Error writing settings.json file: " + err.Error())
		}
	} else {
		defaultCommonSettings[name] = defaultvalue
	}
	return nil
}

// RegisterGlobalOptionPlug creates a new global-only option (named pl.name)
func RegisterGlobalOptionPlug(pl string, name string, defaultvalue interface{}) error {
	return RegisterGlobalOption(pl+"."+name, defaultvalue)
}

// RegisterGlobalOption creates a new global-only option
func RegisterGlobalOption(name string, defaultvalue interface{}) error {
	if v, ok := GlobalSettings[name]; !ok {
		DefaultGlobalOnlySettings[name] = defaultvalue
		GlobalSettings[name] = defaultvalue
		err := WriteSettings(filepath.Join(ConfigDir, "settings.json"))
		if err != nil {
			return errors.New("Error writing settings.json file: " + err.Error())
		}
	} else {
		DefaultGlobalOnlySettings[name] = v
	}
	return nil
}

// GetGlobalOption returns the global value of the given option
func GetGlobalOption(name string) interface{} {
	return GlobalSettings[name]
}

var defaultCommonSettings = map[string]interface{}{
	"autoindent":     true,
	"autosu":         false,
	"backup":         true,
	"backupdir":      "",
	"basename":       false,
	"colorcolumn":    []float64{0},
	"cursorline":     true,
	"diffgutter":     false,
	"encoding":       "utf-8",
	"eofnewline":     true,
	"fastdirty":      false,
	"fileformat":     "unix",
	"filetype":       "unknown",
	"hidecursor":     false,
	"hlsearch":       false,
	"hltaberrors":    false,
	"hltrailingws":   false,
	"incsearch":      true,
	"ignorecase":     true,
	"indentchar":     " ",
	"keepautoindent": false,
	"lsp":            true,
	"lsp-autoimport": false,
	"matchbrace":     true,
	"mkparents":      false,
	"permbackup":     false,
	"readonly":       false,
	"rmtrailingws":   false,
	"ruler":          true,
	"relativeruler":  false,
	"savecursor":     false,
	"saveundo":       false,
	"scrollbar":      false,
	"scrollmargin":   float64(3),
	"scrollspeed":    float64(2),
	"smartpaste":     true,
	"softwrap":       true,
	"splitbottom":    true,
	"splitright":     true,
	"statusformatl":  "$(filename) $(modified)($(line),$(col)) $(status.paste)| ft:$(opt:filetype) | $(opt:fileformat) | $(opt:encoding)",
	"statusformatr":  "$(bind:ToggleKeyMenu): bindings, $(bind:ToggleHelp): help",
	"statusline":     true,
	"syntax":         true,
	"tabmovement":    false,
	"tabsize":        float64(4),
	"tabstospaces":   false,
	"useprimary":     true,
	"wordwrap":       true,
}

func GetInfoBarOffset() int {
	offset := 0
	if GetGlobalOption("infobar").(bool) {
		offset++
	}
	if GetGlobalOption("keymenu").(bool) {
		offset += 2
	}
	return offset
}

// DefaultCommonSettings returns the default global settings for micro
// Note that colorscheme is a global only option
func DefaultCommonSettings() map[string]interface{} {
	commonsettings := make(map[string]interface{})
	for k, v := range defaultCommonSettings {
		commonsettings[k] = v
	}
	return commonsettings
}

// a list of settings that should only be globally modified and their
// default values
var DefaultGlobalOnlySettings = map[string]interface{}{
	"autosave":       float64(0),
	"clipboard":      "external",
	"colorscheme":    "default",
	"divchars":       "|-",
	"divreverse":     true,
	"infobar":        true,
	"keymenu":        false,
	"tabbar":         true,
	"mouse":          true,
	"parsecursor":    false,
	"paste":          false,
	"pluginchannels": []string{"https://raw.githubusercontent.com/micro-editor/plugin-channel/master/channel.json"},
	"pluginrepos":    []string{},
	"savehistory":    true,
	"sucmd":          "sudo",
	"xterm":          false,
}

// a list of settings that should never be globally modified
var LocalSettings = []string{
	"filetype",
	"hidecursor",
	"readonly",
}

// DefaultGlobalSettings returns the default global settings for micro
// Note that colorscheme is a global only option
func DefaultGlobalSettings() map[string]interface{} {
	globalsettings := make(map[string]interface{})
	for k, v := range defaultCommonSettings {
		globalsettings[k] = v
	}
	for k, v := range DefaultGlobalOnlySettings {
		globalsettings[k] = v
	}
	return globalsettings
}

// DefaultAllSettings returns a map of all settings and their
// default values (both common and global settings)
func DefaultAllSettings() map[string]interface{} {
	allsettings := make(map[string]interface{})
	for k, v := range defaultCommonSettings {
		allsettings[k] = v
	}
	for k, v := range DefaultGlobalOnlySettings {
		allsettings[k] = v
	}
	return allsettings
}

var Float64 = reflect.TypeOf(float64(0))
var String = reflect.TypeOf("")

// GetNativeValue parses and validates a value for a given option
func GetNativeValue(option string, realValue interface{}, value string) (interface{}, error) {
	var native interface{}
	rtype := reflect.TypeOf(realValue)
	kind := rtype.Kind()
	if kind == reflect.Bool {
		b, err := util.ParseBool(value)
		if err != nil {
			return nil, ErrInvalidValue
		}
		native = b
	} else if kind == reflect.String {
		native = value
	} else if kind == reflect.Float64 {
		i, err := strconv.Atoi(value)
		if err != nil {
			return nil, ErrInvalidValue
		}
		native = float64(i)
	} else if kind == reflect.Slice {
		value = strings.TrimPrefix(value, "[")
		value = strings.TrimSuffix(value, "]")

		realArray, ok := realValue.([]interface{})
		var eltype reflect.Type = nil
		if ok {
			if len(realArray) > 0 {
				eltype = reflect.TypeOf(realArray[0])
			}
		}

		if (eltype == Float64 || rtype == reflect.SliceOf(Float64)) {
			strvals := strings.Split(value, ",")
			vals := []float64{}
			for _, str := range(strvals) {
				num, err := strconv.Atoi(str)
				if err != nil {
					log.Println("Not a float string")
					return nil, ErrInvalidValue
				}
				vals = append(vals, float64(num))
			}
			native = vals
		} else {
			return nil, ErrInvalidValue
		}
	} else {
		return nil, ErrInvalidValue
	}

	if err := OptionIsValid(option, native); err != nil {
		return nil, errors.New(option + ": expected option " + err.Error())
	}
	return native, nil
}

// OptionIsValid checks if a value is valid for a certain option
func OptionIsValid(option string, value interface{}) error {
	if validator, ok := optionValidators[option]; ok {
		return validator(option, value)
	}

	return nil
}

// Option validators

func ErrExpected(text string) error {
	return errors.New(text)
}

func validateGreater(number float64) optionValidator {
	return func (option string, value interface{}) error {
		val, ok := value.(float64)
		if !ok { return ErrExpected("to be a number")}
		if val > number { return nil }
		return ErrExpected("to be >" + strconv.FormatFloat(number, 'f', -1, 64))
	}
}

func validateLess(number float64) optionValidator {
	return func (option string, value interface{}) error {
		val, ok := value.(float64)
		if !ok { return ErrExpected("to be a number")}
		if val < number { return nil }
		return ErrExpected("to be <" + strconv.FormatFloat(number, 'f', -1, 64))
	}
}

func validateGreaterEqual(number float64) optionValidator {
	return func (option string, value interface{}) error {
		val, ok := value.(float64)
		if !ok { return ErrExpected("to be a number")}
		if val >= number { return nil }
		return ErrExpected("to be >=" + strconv.FormatFloat(number, 'f', -1, 64))
	}
}

func validateLessEqual(number float64) optionValidator {
	return func (option string, value interface{}) error {
		val, ok := value.(float64)
		if !ok { return ErrExpected("to be a number")}
		if val <= number { return nil }
		return ErrExpected("to be <=" + strconv.FormatFloat(number, 'f', -1, 64))
	}
}


func validateAny(validators ...optionValidator) optionValidator {
	return func(option string, value interface{}) error {
		var errs []error
		var succ = false
		for _, validator := range(validators) {
			err := validator(option, value)
			if err != nil { errs = append(errs, err) } else { succ = true }
		}

		if !succ {
			msg := ""
			for i, err := range(errs) {
				if i != 0 { msg += " or " }
				msg += err.Error()
			}

			return ErrExpected(msg)
		}

		return nil
	}
}

func validateAll(validators ...optionValidator) optionValidator {
	return func(option string, value interface{}) error {
		var errs []error
		for _, validator := range(validators) {
			err := validator(option, value)
			if err != nil { errs = append(errs, err) }
		}

		if len(errs) > 0 {
			msg := ""
			for i, err := range(errs) {
				if i != 0 { msg += " and "}
				msg += err.Error()
			}

			return ErrExpected(msg)
		}

		return nil
	}
}

func validateArray(validator optionValidator) optionValidator {
	return func(option string, value interface{}) error {
		list_value := reflect.ValueOf(value)
		if list_value.Kind() != reflect.Slice {
			return ErrExpected("to be an array")
		}

		for i:=0 ; i<list_value.Len(); i++ {
			val := list_value.Index(i)
			err := validator(option, val.Interface())
			if err != nil {
				return ErrExpected("array elements to be " + err.Error())
			}
		}

		return nil
	}
}

func validateType(t reflect.Type) optionValidator {
	return func(option string, value interface{}) error {
		switch reflect.TypeOf(value) {
			case t: return nil
			default: return ErrExpected("to be of type " + t.Name())
		}
	}
}

func validateStringLiteral(lits ...string) optionValidator {
	return func(option string, value interface{}) error {
		val, ok := value.(string)
		if !ok { return ErrExpected("to be a string") }

		for _, lit := range(lits) {
			if val == lit { return nil }
		}

		msg := ""
		for i, lit := range(lits) {
			if i == 0 {
			} else if i == len(lits) - 1 {
				msg += " or "
			} else {
				msg += ", "
			}

			msg += lit
		}

		return ErrExpected("to be " + msg)
	}
}

func validateCalculatedStringLiteral(fn func() []string) optionValidator {
	return func(option string, value interface{}) error {
		return validateStringLiteral(fn()...)(option, value)
	}
}

func validateEncoding(option string, value interface{}) error {
	_, err := htmlindex.Get(value.(string))
	if err != nil { return ErrExpected("to be a valid encoding") }
	return nil
}
