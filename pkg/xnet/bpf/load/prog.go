package load

import (
	"context"
	"fmt"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/flomesh-io/xnet/pkg/xnet/bpf/fs"
	"github.com/flomesh-io/xnet/pkg/xnet/bpf/maps"
	"github.com/flomesh-io/xnet/pkg/xnet/util"
)

const (
	bpftoolCmd            = `bpftool`
	libbpf_strict_feature = `libbpf_strict`
)

var (
	searchBinPaths = []string{
		`/usr/local/bin`,
		`/usr/sbin`,
	}
)

func findBpftoolPath() (bpftoolBin string, legacy bool, err error) {
	for _, binPath := range searchBinPaths {
		bpftoolBin = path.Join(binPath, bpftoolCmd)
		if exists := util.Exists(bpftoolBin); exists {
			break
		}
	}

	if len(bpftoolBin) > 0 {
		args := []string{
			`version`,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bpftoolBin, args...) // nolint gosec
		if output, e := cmd.Output(); e == nil && len(output) > 0 {
			if strings.Contains(string(output), libbpf_strict_feature) {
				legacy = true
			}
		}
	} else {
		err = fmt.Errorf("fail to find %s", bpftoolCmd)
	}

	return
}

func ProgLoadAll() {
	pinningDir := fs.GetPinningDir()
	if exists := util.Exists(pinningDir); exists {
		return
	}
	args := []string{
		`prog`,
		`loadall`,
		bpfProgPath,
		pinningDir,
		`pinmaps`,
		pinningDir,
	}

	bpftoolBin, legacy, err := findBpftoolPath()
	if err != nil {
		log.Fatal().Err(err).Msgf("fail to find %s", bpftoolCmd)
		return
	}

	if legacy {
		args = append(args, `--legacy`)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bpftoolBin, args...) // nolint gosec
	if output, err := cmd.Output(); err != nil {
		log.Debug().Msg(err.Error())
	} else if len(output) > 0 {
		log.Debug().Msg(string(output))
	}

	maps.InitProgEntries()
}

func InitMeshConfig() {
	if cfgVal, cfgErr := maps.GetXNetCfg(maps.SysMesh); cfgErr != nil {
		log.Fatal().Msg(cfgErr.Error())
	} else {
		if !cfgVal.IPv4().IsSet(maps.CfgFlagOffsetUDPProtoAllowAll) {
			if !cfgVal.IPv4().IsSet(maps.CfgFlagOffsetUDPProtoDenyAll) &&
				!cfgVal.IPv4().IsSet(maps.CfgFlagOffsetUDPNatByIpPortOn) &&
				!cfgVal.IPv4().IsSet(maps.CfgFlagOffsetUDPNatByIpOn) &&
				!cfgVal.IPv4().IsSet(maps.CfgFlagOffsetUDPNatByPortOn) &&
				!cfgVal.IPv4().IsSet(maps.CfgFlagOffsetUDPNatAllOff) {
				cfgVal.IPv4().Set(maps.CfgFlagOffsetUDPProtoAllowAll)
			}
		}
		cfgVal.IPv4().Set(maps.CfgFlagOffsetAclCheckOn)
		if cfgErr = maps.SetXNetCfg(maps.SysMesh, cfgVal); cfgErr != nil {
			log.Fatal().Msg(cfgErr.Error())
		}
	}
}

func InitE4lbConfig(enableE4lbIPv4, enableE4lbIPv6 bool) {
	if cfgVal, cfgErr := maps.GetXNetCfg(maps.SysE4lb); cfgErr != nil {
		log.Fatal().Msg(cfgErr.Error())
	} else {
		if enableE4lbIPv4 {
			cfgVal.IPv4().Set(maps.CfgFlagOffsetTCPNatAllOff)
			cfgVal.IPv4().Set(maps.CfgFlagOffsetTCPNatByIpPortOn)
			cfgVal.IPv4().Set(maps.CfgFlagOffsetTCPProtoAllowNatEscape)
			cfgVal.IPv4().Set(maps.CfgFlagOffsetUDPProtoAllowAll)
			cfgVal.IPv4().Clear(maps.CfgFlagOffsetOTHProtoDenyAll)
		} else {
			cfgVal.IPv4().Clear(maps.CfgFlagOffsetDenyAll)
		}

		if enableE4lbIPv6 {
			cfgVal.IPv6().Set(maps.CfgFlagOffsetTCPNatAllOff)
			cfgVal.IPv6().Set(maps.CfgFlagOffsetTCPNatByIpPortOn)
			cfgVal.IPv6().Set(maps.CfgFlagOffsetTCPProtoAllowNatEscape)
			cfgVal.IPv6().Set(maps.CfgFlagOffsetUDPProtoAllowAll)
			cfgVal.IPv6().Clear(maps.CfgFlagOffsetOTHProtoDenyAll)
		} else {
			cfgVal.IPv6().Clear(maps.CfgFlagOffsetDenyAll)
		}

		if cfgErr = maps.SetXNetCfg(maps.SysE4lb, cfgVal); cfgErr != nil {
			log.Fatal().Msg(cfgErr.Error())
		}
	}
}
