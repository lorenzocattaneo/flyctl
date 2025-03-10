package flag

import (
	"context"
	"time"

	"github.com/spf13/pflag"
)

type contextKey struct{}

// NewContext derives a context that carries fs from ctx.
func NewContext(ctx context.Context, fs *pflag.FlagSet) context.Context {
	return context.WithValue(ctx, contextKey{}, fs)
}

// FromContext returns the FlagSet ctx carries. It panics in case ctx carries
// no FlagSet.
func FromContext(ctx context.Context) *pflag.FlagSet {
	return ctx.Value(contextKey{}).(*pflag.FlagSet)
}

// Args is shorthand for FromContext(ctx).Args().
func Args(ctx context.Context) []string {
	return FromContext(ctx).Args()
}

// FirstArg returns the first arg ctx carries or an empty string in case ctx
// carries an empty argument set. It panics in case ctx carries no FlagSet.
func FirstArg(ctx context.Context) string {
	if args := Args(ctx); len(args) > 0 {
		return args[0]
	}

	return ""
}

// GetString returns the value of the named string flag ctx carries.
func GetString(ctx context.Context, name string) string {
	if v, err := FromContext(ctx).GetString(name); err != nil {
		return ""
	} else {
		return v
	}
}

// GetInt returns the value of the named int flag ctx carries. It panics
// in case ctx carries no flags or in case the named flag isn't an int one.
func GetInt(ctx context.Context, name string) int {
	if v, err := FromContext(ctx).GetInt(name); err != nil {
		panic(err)
	} else {
		return v
	}
}

// GetString returns the value of the named string flag ctx carries.
func GetStringSlice(ctx context.Context, name string) []string {
	if v, err := FromContext(ctx).GetStringSlice(name); err != nil {
		return []string{}
	} else {
		return v
	}
}

// GetDuration returns the value of the named duration flag ctx carries.
func GetDuration(ctx context.Context, name string) time.Duration {
	if v, err := FromContext(ctx).GetDuration(name); err != nil {
		return 0
	} else {
		return v
	}
}

// GetBool returns the value of the named boolean flag ctx carries.
func GetBool(ctx context.Context, name string) bool {
	if v, err := FromContext(ctx).GetBool(name); err != nil {
		return false
	} else {
		return v
	}
}

// IsSpecified returns whether a flag has been specified at all or not.
// This is useful, for example, when differentiating between 0/"" and unspecified.
func IsSpecified(ctx context.Context, name string) bool {
	flag := FromContext(ctx).Lookup(name)
	return flag != nil && flag.Changed
}

// GetOrg is shorthand for GetString(ctx, OrgName).
func GetOrg(ctx context.Context) string {
	return GetString(ctx, OrgName)
}

// GetRegion is shorthand for GetString(ctx, RegionName).
func GetRegion(ctx context.Context) string {
	return GetString(ctx, RegionName)
}

// GetYes is shorthand for GetBool(ctx, YesName).
func GetYes(ctx context.Context) bool {
	return GetBool(ctx, YesName)
}

// GetApp is shorthand for GetString(ctx, AppName).
func GetApp(ctx context.Context) string {
	return GetString(ctx, AppName)
}

// GetAppConfigFilePath is shorthand for GetString(ctx, AppConfigFilePathName).
func GetAppConfigFilePath(ctx context.Context) string {
	if path, err := FromContext(ctx).GetString(AppConfigFilePathName); err != nil {
		return ""
	} else {
		return path
	}
}
