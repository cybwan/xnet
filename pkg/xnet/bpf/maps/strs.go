package maps

import (
	"net"
	"strings"
	"time"

	"github.com/flomesh-io/xnet/pkg/xnet/util"
)

func SysName(sysId SysID) string {
	return _sys_(uint32(sysId))
}

func _sys_(sys uint32) string {
	switch sys {
	case uint32(SysNoop):
		return "Noop"
	case uint32(SysMesh):
		return "Mesh"
	case uint32(SysE4lb):
		return "E4lb"
	default:
		return ""
	}
}

func _duration_(atime uint64) string {
	escape := time.Duration(util.Uptime())*time.Second - time.Duration(atime)*time.Nanosecond
	return escape.String()
}

func _ip_(ipNb [4]uint32) string {
	if ipNb[1] == 0 && ipNb[2] == 0 && ipNb[3] == 0 {
		return util.IntToIPv4(ipNb[0]).String()
	} else {
		return util.Int4ToIPv6(ipNb).String()
	}
}

func _port_(port uint16) uint16 {
	return util.NetToHostShort(port)
}

func _mac_(mac []uint8) string {
	hwAddr := net.HardwareAddr(mac[:])
	return hwAddr.String()
}

func _proto_(proto uint8) string {
	switch proto {
	case uint8(IPPROTO_TCP):
		return "IPPROTO_TCP"
	case uint8(IPPROTO_UDP):
		return "IPPROTO_UDP"
	default:
		return ""
	}
}

func _bool_(v uint8) bool {
	return v == 1
}

func _tc_dir_(tcDir uint8) string {
	switch tcDir {
	case uint8(TC_DIR_IGR):
		return "TC_DIR_IGR"
	case uint8(TC_DIR_EGR):
		return "TC_DIR_EGR"
	default:
		return ""
	}
}

func _acl_(acl uint8) string {
	switch acl {
	case uint8(ACL_DENY):
		return "ACL_DENY"
	case uint8(ACL_AUDIT):
		return "ACL_AUDIT"
	case uint8(ACL_TRUSTED):
		return "ACL_TRUSTED"
	default:
		return ""
	}
}

func _flow_dir_(flowDir uint8) string {
	switch flowDir {
	case 0:
		return "FLOW_DIR_C2S"
	case 1:
		return "FLOW_DIR_S2C"
	default:
		return ""
	}
}

func _nf_(nf uint8) string {
	if nf == 0 {
		return "NF_DENY"
	}

	desc := ""
	if nf&1 == 1 {
		desc += "NF_ALLOW "
	}
	if nf&2 == 2 {
		desc += "NF_XNAT "
	}
	if nf&4 == 4 {
		desc += "NF_RDIR "
	}
	if nf&8 == 8 {
		desc += "NF_SKIP_SM "
	}
	return strings.TrimSpace(desc)
}

func _tcp_state_(state uint8) string {
	switch state {
	case 0x0:
		return "TCP_STATE_CLOSED"
	case 0x1:
		return "TCP_STATE_SYN_SEND"
	case 0x2:
		return "TCP_STATE_SYN_ACK"
	case 0x4:
		return "TCP_STATE_EST"
	case 0x08:
		return "TCP_STATE_ERR"
	case 0x10:
		return "TCP_STATE_FINI"
	case 0x20:
		return "TCP_STATE_FIN2"
	case 0x40:
		return "TCP_STATE_FIN3"
	case 0x80:
		return "TCP_STATE_CWT"
	default:
		return ""
	}
}

func _write_(sb *strings.Builder, str string) {
	if cnt, err := sb.WriteString(str); err != nil {
		log.Error().Err(err).Msgf("fail to write string: %s", str)
	} else if cnt != len(str) {
		log.Error().Msgf("fail to write string: %s", str)
	}
}
