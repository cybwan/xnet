package controller

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"

	"github.com/flomesh-io/xnet/pkg/k8s"
	"github.com/flomesh-io/xnet/pkg/messaging"
	"github.com/flomesh-io/xnet/pkg/version"
	"github.com/flomesh-io/xnet/pkg/xnet/bpf/load"
	"github.com/flomesh-io/xnet/pkg/xnet/bpf/maps"
	"github.com/flomesh-io/xnet/pkg/xnet/cni"
	"github.com/flomesh-io/xnet/pkg/xnet/cni/deliver"
	"github.com/flomesh-io/xnet/pkg/xnet/e4lb"
	"github.com/flomesh-io/xnet/pkg/xnet/volume"
)

type server struct {
	ctx            context.Context
	kubeController k8s.Controller
	msgBroker      *messaging.Broker
	stop           chan struct{}

	enableE4lb     bool
	enableE4lbIPv4 bool
	enableE4lbIPv6 bool

	enableMesh bool

	upgradeProg   bool
	uninstallProg bool

	meshCfgIPv4Magic string
	meshCfgIPv6Magic string
	e4lbCfgIPv4Magic string
	e4lbCfgIPv6Magic string

	unixSockPath string
	cniReady     chan struct{}

	meshFilterPortInbound  string
	meshFilterPortOutbound string

	flushTCPConnTrackCrontab     string
	flushTCPConnTrackIdleSeconds int
	flushTCPConnTrackBatchSize   int

	flushUDPConnTrackCrontab     string
	flushUDPConnTrackIdleSeconds int
	flushUDPConnTrackBatchSize   int

	cniBridges []net.Interface
}

// NewServer returns a new CNI Server.
// the path this the unix path to listen.
func NewServer(ctx context.Context,
	kubeController k8s.Controller, msgBroker *messaging.Broker, stop chan struct{},
	enableE4lb, enableE4lbIPv4, enableE4lbIPv6, enableMesh, upgradeProg, uninstallProg bool, cniBridges []net.Interface,
	meshCfgIPv4Magic, meshCfgIPv6Magic, e4lbCfgIPv4Magic, e4lbCfgIPv6Magic string,
	meshFilterPortInbound, meshFilterPortOutbound string,
	flushTCPConnTrackCrontab string, flushTCPConnTrackIdleSeconds, flushTCPConnTrackBatchSize int,
	flushUDPConnTrackCrontab string, flushUDPConnTrackIdleSeconds, flushUDPConnTrackBatchSize int) Server {
	return &server{
		unixSockPath:   cni.GetCniSock(volume.SysRun.MountPath),
		kubeController: kubeController,
		msgBroker:      msgBroker,
		cniReady:       make(chan struct{}, 1),
		ctx:            ctx,
		stop:           stop,

		enableE4lb:     enableE4lb,
		enableE4lbIPv4: enableE4lbIPv4,
		enableE4lbIPv6: enableE4lbIPv6,

		enableMesh: enableMesh,

		upgradeProg:   upgradeProg,
		uninstallProg: uninstallProg,

		meshCfgIPv4Magic: meshCfgIPv4Magic,
		meshCfgIPv6Magic: meshCfgIPv6Magic,
		e4lbCfgIPv4Magic: e4lbCfgIPv4Magic,
		e4lbCfgIPv6Magic: e4lbCfgIPv6Magic,

		meshFilterPortInbound:  meshFilterPortInbound,
		meshFilterPortOutbound: meshFilterPortOutbound,

		flushTCPConnTrackCrontab:     flushTCPConnTrackCrontab,
		flushTCPConnTrackIdleSeconds: flushTCPConnTrackIdleSeconds,
		flushTCPConnTrackBatchSize:   flushTCPConnTrackBatchSize,

		flushUDPConnTrackCrontab:     flushUDPConnTrackCrontab,
		flushUDPConnTrackIdleSeconds: flushUDPConnTrackIdleSeconds,
		flushUDPConnTrackBatchSize:   flushUDPConnTrackBatchSize,

		cniBridges: cniBridges,
	}
}

func (s *server) Start() error {
	if s.upgradeProg || s.uninstallProg {
		e4lb.E4lbOff()
		s.uninstallCNI()
		s.checkAndResetPods()
		load.ProgUnload()
	}

	r := mux.NewRouter()
	r.Path(cni.VersionURI).
		Methods("GET").
		HandlerFunc(version.VersionHandler)

	if !s.uninstallProg {
		load.ProgLoad()
		s.loadBridges()

		if !s.enableE4lb {
			e4lb.E4lbOff()
		} else {
			load.InitE4lbConfig(s.enableE4lbIPv4, s.enableE4lbIPv6, s.e4lbCfgIPv4Magic, s.e4lbCfgIPv6Magic)
			s.checkAndRepairE4lb()
		}

		if !s.enableMesh {
			s.uninstallCNI()
			go s.checkAndResetPods()
		} else {
			load.InitMeshConfig(s.meshCfgIPv4Magic, s.meshCfgIPv6Magic)

			r.Path(cni.CreatePodURI).
				Methods("POST").
				HandlerFunc(s.PodCreated)

			r.Path(cni.DeletePodURI).
				Methods("POST").
				HandlerFunc(s.PodDeleted)

			s.installCNI()

			// wait for cni to be ready
			<-s.cniReady

			go s.broadcastListener()

			go s.checkAndRepairPods()

			if len(s.flushTCPConnTrackCrontab) > 0 && s.flushTCPConnTrackIdleSeconds > 0 && s.flushTCPConnTrackBatchSize > 0 {
				go s.idleTCPConnTrackFlush(maps.SysMesh)
			}

			if len(s.flushUDPConnTrackCrontab) > 0 && s.flushUDPConnTrackIdleSeconds > 0 && s.flushUDPConnTrackBatchSize > 0 {
				go s.idleUDPConnTrackFlush(maps.SysMesh)
			}
		}
	}

	if err := os.RemoveAll(s.unixSockPath); err != nil {
		log.Fatal().Msg(err.Error())
	}
	listen, err := net.Listen("unix", s.unixSockPath)
	if err != nil {
		log.Fatal().Msgf("listen error:%v", err)
	}

	ss := http.Server{
		Handler:           r,
		WriteTimeout:      10 * time.Second,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		go ss.Serve(listen) // nolint: errcheck
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT, syscall.SIGABRT)
		select {
		case <-ch:
			s.Stop()
		case <-s.stop:
			s.Stop()
		}
		_ = ss.Shutdown(s.ctx)
	}()

	return nil
}

func (s *server) installCNI() {
	install := deliver.NewInstaller(`/app`)
	go func() {
		if err := install.Run(context.TODO(), s.cniReady); err != nil {
			close(s.cniReady)
			log.Fatal().Msg(err.Error())
		}
		if err := install.Cleanup(context.TODO()); err != nil {
			log.Error().Msgf("Failed to clean up CNI: %v", err)
		}
	}()

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT, syscall.SIGABRT)
		<-ch
		if err := install.Cleanup(context.TODO()); err != nil {
			log.Error().Msgf("Failed to clean up CNI: %v", err)
		}
	}()
}

func (s *server) uninstallCNI() {
	install := deliver.NewInstaller(`/app`)
	if err := install.Cleanup(context.TODO()); err != nil {
		log.Error().Msgf("Failed to clean up CNI: %v", err)
	}
}

func (s *server) Stop() {
	log.Info().Msg("cni-server stop ...")
	close(s.stop)
}
