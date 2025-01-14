package e4lb

import (
	"net"

	"github.com/flomesh-io/xnet/pkg/xnet/bpf/maps"
	"github.com/flomesh-io/xnet/pkg/xnet/tc"
	"github.com/flomesh-io/xnet/pkg/xnet/util/link"
)

func BridgeOn() {
	if success := link.LinkTapAdd(flbDev); !success {
		log.Error().Msgf("fail to add %s link", flbDev)
	} else {
		if iface, ifaceErr := net.InterfaceByName(flbDev); ifaceErr != nil {
			log.Error().Err(ifaceErr).Msgf("fail to find %s link", flbDev)
		} else {
			if attachErr := tc.AttachBPFProg(maps.SysE4lb, iface.Name); attachErr != nil {
				log.Error().Err(attachErr).Msgf("fail to attach %s link", flbDev)
			}
		}
	}
}
