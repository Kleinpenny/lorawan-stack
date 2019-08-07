// Copyright © 2019 The Things Industries B.V.

package rpcserver

import (
	"go.thethings.network/lorawan-stack/pkg/rpcmiddleware/validator"
	"go.thethings.network/lorawan-stack/pkg/tenant"
	"go.thethings.network/lorawan-stack/pkg/ttipb"
)

func init() {
	for rpc, paths := range ttipb.AllowedFieldMaskPathsForRPC {
		validator.RegisterAllowedFieldMaskPaths(rpc, paths...)
	}
}

// WithTenantConfig adds tenant configuration.
func WithTenantConfig(config tenant.Config) Option {
	return func(o *options) {
		o.tenant = config
	}
}