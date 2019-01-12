package log

import (
	"fmt"
	stdlog "log"
	"net/http"
	"os"
	"strings"
	"time"

	raven "github.com/getsentry/raven-go"
	"github.com/tchap/zapext/zapsentry"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	VerboseLevel zapcore.Level = zapcore.DebugLevel - 1 + iota
	DebugLevel
	SuccessLevel
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

var activeLogLevel = DebugLevel

var (
	Log *zap.SugaredLogger
	// Debug, Info, Error, Fatal func(string, ...interface{})
)

func init() {
	EnableLogging()
}

var logLevelMap = map[string]zapcore.Level{
	"v":       VerboseLevel,
	"verbose": VerboseLevel,

	"d":     DebugLevel,
	"dbg":   DebugLevel,
	"debug": DebugLevel,

	"success": SuccessLevel,
	"ok":      SuccessLevel,

	"standard": InfoLevel,
	"std":      InfoLevel,
	"info":     InfoLevel,

	"w":        WarnLevel,
	"warn":     WarnLevel,
	"warning":  WarnLevel,
	"warnings": WarnLevel,

	"e":      ErrorLevel,
	"err":    ErrorLevel,
	"error":  ErrorLevel,
	"errors": ErrorLevel,
}

func EnableLogging() {
	initZapLogger(
		os.Getenv("LOGHUB_SINK") == "enabled",
		os.Getenv("SENTRY_SINK") != "",
	)
	whitelist := strings.Trim(os.Getenv("LOG_WHITELIST"), `"'`)
	blacklist := strings.Trim(os.Getenv("LOG_BLACKLIST"), `"'`)
	for _, s := range strings.Split(whitelist, ",") {
		f := strings.Split(s, "=")
		if len(f) < 2 {
			continue
		}
		module := strings.TrimSpace(f[0])
		lvl, ok := logLevelMap[strings.TrimSpace(f[1])]
		if !ok {
			fmt.Printf("CANNOT INTERPRET LOG_LEVEL SPEC in LOG_WHITELIST: '%s'\n", s)
			continue
		}
		TurnUpModuleToLevel(module, zapcore.Level(lvl))
	}
	for _, s := range strings.Split(blacklist, ",") {
		f := strings.Split(s, "=")
		if len(f) < 2 {
			continue
		}
		module := strings.TrimSpace(f[0])
		lvl, ok := logLevelMap[strings.TrimSpace(f[1])]
		if !ok {
			fmt.Printf("CANNOT INTERPRET LOG_LEVEL SPEC in LOG_BLACKLIST: '%s'\n", s)
			continue
		}
		MuteModuleToLevel(module, zapcore.Level(lvl))
	}
}

func SetLogLevel(lvl zapcore.Level) {
	activeLogLevel = lvl
}

func DisableLogging() {
	noLogs := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return false
	})
	config := zap.NewDevelopmentEncoderConfig()
	jsonEncoder := zapcore.NewJSONEncoder(config)
	consoleOutput := zapcore.Lock(os.Stderr)
	core := zapcore.NewCore(jsonEncoder, consoleOutput, noLogs)
	Log = zap.New(core).Sugar()
}

var moduleWhitelist = map[string]zapcore.Level{}
var moduleBlacklist = map[string]zapcore.Level{}

func MuteModuleToLevel(module string, logLevel zapcore.Level) {
	moduleBlacklist[module] = logLevel
	fmt.Println("muted log level for module", module, "from", logLevel)
}
func TurnUpModuleToLevel(module string, logLevel zapcore.Level) {
	delete(moduleBlacklist, module)
	moduleWhitelist[module] = logLevel
	fmt.Println("turned up log level for module", module, "to", logLevel)
}

func Verbose(msg string, keysAndValues ...interface{}) {
	Output(VerboseLevel, msg, keysAndValues...)
}
func Debug(msg string, keysAndValues ...interface{}) {
	Output(DebugLevel, msg, keysAndValues...)
}
func Success(msg string, keysAndValues ...interface{}) {
	Output(SuccessLevel, msg, keysAndValues...)
}
func Info(msg string, keysAndValues ...interface{}) {
	Output(InfoLevel, msg, keysAndValues...)
}
func Warn(msg string, keysAndValues ...interface{}) {
	Log.Warnw(msg, keysAndValues...)
}
func Error(msg string, keysAndValues ...interface{}) {
	Log.Errorw(msg, keysAndValues...)
}
func Fatal(msg string, keysAndValues ...interface{}) {
	Log.Fatalw(msg, keysAndValues...)
}
func Panicf(msg string, keysAndValues ...interface{}) {
	stdlog.Panicf(msg, keysAndValues...)
}

func Output(logLevel zapcore.Level, msg string, keysAndValues ...interface{}) {
	if len(keysAndValues) >= 2 && len(moduleBlacklist) > 0 {
		if k, ok := keysAndValues[0].(string); ok && k == "func" {
			if v, ok := keysAndValues[1].(string); ok {
				if idx := strings.IndexByte(v, '.'); idx != -1 {
					module := v[:idx]
					if blacklistedLevel, ok := moduleBlacklist[module]; ok {
						if logLevel <= blacklistedLevel {
							return // ignore log
						}
					}
					if whitelistedLevel, ok := moduleWhitelist[module]; ok {
						if logLevel >= whitelistedLevel {
							Log.Debugw(msg, keysAndValues...)
							return
						}
					}
				}
			}
		}
	}
	if activeLogLevel > logLevel {
		return
	}
	Log.Debugw(msg, keysAndValues...)
}

func initZapLogger(withLoghub, withSentry bool) {
	//	logger, err := zap.NewDevelopment()
	//	if err != nil {
	//		panic(err)
	//	}

	// Log filter
	allLogs := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		// return lvl < zapcore.ErrorLevel
		return true
	})

	// Log output encoding
	// textEncoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
	config := zap.NewDevelopmentEncoderConfig()
	jsonEncoder := zapcore.NewJSONEncoder(config)

	// Log sinks

	//   Loghub over HTTP sink
	var loghubOutput zapcore.WriteSyncer
	if withLoghub {
		writer := new(LoghubWriter)
		writer.client = &http.Client{
			Timeout: 250 * time.Millisecond,
		}
		loghubWriter := zapcore.AddSync(writer)
		loghubOutput = zapcore.Lock(loghubWriter)
	}

	//   Basic STDERR output sink
	consoleOutput := zapcore.Lock(os.Stderr)

	var core zapcore.Core

	if withLoghub {
		core = zapcore.NewTee(
			zapcore.NewCore(jsonEncoder, consoleOutput, allLogs),
			zapcore.NewCore(jsonEncoder, loghubOutput, allLogs),
		)
	} else if withSentry {
		dsn := os.Getenv("SENTRY_SINK")
		if dsn == "" {
			panic("requested Sentry logging, but SENTRY_SINK is not set in env")
		}

		dsn = strings.Trim(dsn, "'")

		fmt.Printf("Sending Zap logs to Sentry at: %s\n", dsn)

		client, err := raven.New(dsn)
		if err != nil {
			panic(err)
		}

		core = zapcore.NewTee(
			zapcore.NewCore(jsonEncoder, consoleOutput, allLogs),
			zapsentry.NewCore(allLogs, client),
		)
	} else {
		core = zapcore.NewCore(jsonEncoder, consoleOutput, allLogs)
	}

	Log = zap.New(core).Sugar()
}
