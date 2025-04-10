package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"path"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/flomesh-io/xnet/pkg/k8s"
	"github.com/flomesh-io/xnet/pkg/k8s/informers"
	"github.com/flomesh-io/xnet/pkg/logger"
	"github.com/flomesh-io/xnet/pkg/messaging"
	"github.com/flomesh-io/xnet/pkg/signals"
	"github.com/flomesh-io/xnet/pkg/version"
	"github.com/flomesh-io/xnet/pkg/xnet/cni/controller"
	"github.com/flomesh-io/xnet/pkg/xnet/volume"
)

var (
	verbosity    string
	meshName     string // An ID that uniquely identifies an FSM instance
	fsmVersion   string
	fsmNamespace string

	enableMesh     bool
	enableE4lb     bool
	enableE4lbIPv4 bool
	enableE4lbIPv6 bool

	upgradeProg   bool
	uninstallProg bool

	meshCfgIPv4Magic string
	meshCfgIPv6Magic string
	e4lbCfgIPv4Magic string
	e4lbCfgIPv6Magic string

	meshFilterPortInbound  string
	meshFilterPortOutbound string
	meshExcludeNamespaces  []string

	flushTCPConnTrackCrontab     string
	flushTCPConnTrackIdleSeconds int
	flushTCPConnTrackBatchSize   int

	flushUDPConnTrackCrontab     string
	flushUDPConnTrackIdleSeconds int
	flushUDPConnTrackBatchSize   int

	nodePathCniBin  string
	nodePathCniNetd string
	nodePathSysFs   string
	nodePathSysRun  string

	cniIPv4BridgeName string
	cniIPv4BridgeMac  string
	cniIPv6BridgeName string
	cniIPv6BridgeMac  string

	rtScheme = runtime.NewScheme()

	flags = pflag.NewFlagSet(`fsm-xnet`, pflag.ExitOnError)
	log   = logger.New("fsm-xnet-switcher")
)

func init() {
	flags.StringVarP(&verbosity, "verbosity", "v", "info", "Set log verbosity level")
	flags.StringVar(&meshName, "mesh-name", "", "FSM mesh name")
	flags.StringVar(&fsmVersion, "fsm-version", "", "Version of FSM")
	flags.StringVar(&fsmNamespace, "fsm-namespace", "", "FSM controller's namespace")

	flags.BoolVar(&enableMesh, "enable-mesh", true, "Enable service mesh")
	flags.BoolVar(&enableE4lb, "enable-e4lb", false, "Enable 4-layer load balance")
	flags.BoolVar(&enableE4lbIPv4, "enable-e4lb-ipv4", true, "Enable 4-layer load balance with ipv4")
	flags.BoolVar(&enableE4lbIPv6, "enable-e4lb-ipv6", true, "Enable 4-layer load balance with ipv6")

	flags.BoolVar(&upgradeProg, "upgrade-prog", false, "Upgrade xnet prog")
	flags.BoolVar(&uninstallProg, "uninstall-prog", false, "Uninstall xnet prog")

	flags.StringVar(&meshCfgIPv4Magic, "mesh-cfg-ipv4-magic", "", "mesh ipv4 config magic")
	flags.StringVar(&meshCfgIPv6Magic, "mesh-cfg-ipv6-magic", "", "mesh ipv6 config magic")
	flags.StringVar(&e4lbCfgIPv4Magic, "e4lb-cfg-ipv4-magic", "", "e4lb ipv4 config magic")
	flags.StringVar(&e4lbCfgIPv6Magic, "e4lb-cfg-ipv6-magic", "", "e4lb ipv6 config magic")

	flags.StringVar(&meshFilterPortInbound, "mesh-filter-port-inbound", "inbound", "mesh filter inbound port flag")
	flags.StringVar(&meshFilterPortOutbound, "mesh-filter-port-outbound", "outbound", "mesh filter outbound port flag")
	flags.StringArrayVar(&meshExcludeNamespaces, "mesh-exclude-namespace", nil, "mesh exclude namespaces")

	flags.StringVar(&flushTCPConnTrackCrontab, "flush-tcp-conn-track-cron-tab", "30 3 */1 * *", "flush tcp conn track cron tab")
	flags.IntVar(&flushTCPConnTrackIdleSeconds, "flush-tcp-conn-track-idle-seconds", 3600, "flush tcp flow idle seconds")
	flags.IntVar(&flushTCPConnTrackBatchSize, "flush-tcp-conn-track-batch-size", 4096, "flush tcp flow batch size")

	flags.StringVar(&flushUDPConnTrackCrontab, "flush-udp-conn-track-cron-tab", "*/2 * * * *", "flush udp conn track cron tab")
	flags.IntVar(&flushUDPConnTrackIdleSeconds, "flush-udp-conn-track-idle-seconds", 120, "flush udp conn track idle seconds")
	flags.IntVar(&flushUDPConnTrackBatchSize, "flush-udp-conn-track-batch-size", 4096, "flush udp conn track batch size")

	flags.StringVar(&nodePathCniBin, "node-path-cni-bin", "", "cni bin node path")
	flags.StringVar(&nodePathCniNetd, "node-path-cni-netd", "", "cni net-d node path")
	flags.StringVar(&nodePathSysFs, "node-path-sys-fs", "", "sys fs node path")
	flags.StringVar(&nodePathSysRun, "node-path-sys-run", "", "sys run node path")

	flags.StringVar(&cniIPv4BridgeName, "cni-ipv4-bridge-name", "", "cni ipv4 bridge name")
	flags.StringVar(&cniIPv4BridgeMac, "cni-ipv4-bridge-mac", "", "cni ipv4 bridge mac")
	flags.StringVar(&cniIPv6BridgeName, "cni-ipv6-bridge-name", "", "cni ipv6 bridge name")
	flags.StringVar(&cniIPv6BridgeMac, "cni-ipv6-bridge-mac", "", "cni ipv6 bridge mac")

	_ = scheme.AddToScheme(rtScheme)
}

func parseFlags() error {
	if err := flags.Parse(os.Args); err != nil {
		return err
	}
	_ = flag.CommandLine.Parse([]string{})

	if len(nodePathCniBin) > 0 {
		volume.CniBin.HostPath = nodePathCniBin
	}
	if len(nodePathCniNetd) > 0 {
		volume.CniNetd.HostPath = nodePathCniNetd
	}
	if len(nodePathSysFs) > 0 {
		volume.Sysfs.HostPath = nodePathSysFs
	}
	if len(nodePathSysRun) > 0 {
		volume.SysRun.HostPath = nodePathSysRun

		netnsDir := path.Join(volume.SysRun.HostPath, `docker`, `netns`)
		if _, err := os.ReadDir(netnsDir); err == nil {
			volume.Netns = append(volume.Netns, netnsDir)
		}

		netnsDir = path.Join(volume.SysRun.HostPath, `netns`)
		if _, err := os.ReadDir(netnsDir); err == nil {
			volume.Netns = append(volume.Netns, netnsDir)
		}

		netnsDir = volume.SysProc.MountPath
		if _, err := os.ReadDir(netnsDir); err == nil {
			volume.Netns = append(volume.Netns, netnsDir)
		}
	}

	return nil
}

// validateCLIParams contains all checks necessary that various permutations of the CLI flags are consistent
func validateCLIParams() error {
	if meshName == "" {
		return fmt.Errorf("please specify the mesh name using --mesh-name")
	}

	if fsmNamespace == "" {
		return fmt.Errorf("please specify the FSM namespace using --fsm-namespace")
	}

	return nil
}

func main() {
	log.Info().Msgf("Starting fsm-xnet-switcher %s; %s; %s", version.Version, version.GitCommit, version.BuildDate)
	if err := parseFlags(); err != nil {
		log.Fatal().Err(err).Msg("Error parsing cmd line arguments")
	}
	if err := logger.SetLogLevel(verbosity); err != nil {
		log.Fatal().Err(err).Msg("Error setting log level")
	}

	// This ensures CLI parameters (and dependent values) are correct.
	if err := validateCLIParams(); err != nil {
		log.Fatal().Err(err).Msg("Error validating CLI parameters")
	}

	// Initialize kube config and client
	kubeConfig, err := clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		log.Fatal().Err(err).Msg("Error creating kube configs using in-cluster config")
	}
	kubeClient := kubernetes.NewForConfigOrDie(kubeConfig)

	opts := []informers.InformerCollectionOption{
		informers.WithKubeClient(kubeClient),
	}

	ctx, cancel := context.WithCancel(context.Background())
	stop := signals.RegisterExitHandlers(cancel)

	msgBroker := messaging.NewBroker(stop)

	informerCollection, err := informers.NewInformerCollection(meshName, fsmNamespace, stop, opts...)
	if err != nil {
		log.Fatal().Err(err).Msg("Error creating informer collection")
	}

	kubeController := k8s.NewKubernetesController(informerCollection, msgBroker, meshExcludeNamespaces)

	cniBridges := make([]net.Interface, 0)
	if len(cniIPv4BridgeName) > 0 {
		cni4Br := net.Interface{Name: cniIPv4BridgeName}
		if len(cniIPv4BridgeMac) > 0 {
			cni4Br.HardwareAddr, _ = net.ParseMAC(cniIPv4BridgeMac)
		}
		cniBridges = append(cniBridges, cni4Br)
	}
	if len(cniIPv6BridgeName) > 0 {
		cni6Br := net.Interface{Name: cniIPv6BridgeName}
		if len(cniIPv6BridgeMac) > 0 {
			cni6Br.HardwareAddr, _ = net.ParseMAC(cniIPv6BridgeMac)
		}
		cniBridges = append(cniBridges, cni6Br)
	}

	server := controller.NewServer(ctx, kubeController, msgBroker, stop,
		enableE4lb, enableE4lbIPv4, enableE4lbIPv6, enableMesh,
		upgradeProg, uninstallProg, cniBridges,
		meshCfgIPv4Magic, meshCfgIPv6Magic, e4lbCfgIPv4Magic, e4lbCfgIPv6Magic,
		meshFilterPortInbound, meshFilterPortOutbound,
		flushTCPConnTrackCrontab, flushTCPConnTrackIdleSeconds, flushTCPConnTrackBatchSize,
		flushUDPConnTrackCrontab, flushUDPConnTrackIdleSeconds, flushUDPConnTrackBatchSize)
	if err = server.Start(); err != nil {
		log.Fatal().Msg(err.Error())
	}

	<-stop

	log.Info().Msgf("Stopping fsm-xnet-switcher %s; %s; %s", version.Version, version.GitCommit, version.BuildDate)
}
