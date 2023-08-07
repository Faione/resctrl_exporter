package collectors

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Faione/easyxporter"
	"github.com/opencontainers/runc/libcontainer/intelrdt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

func init() {
	easyxporter.RegisterCollector(resctrl, true, NewResctrlStatCollector)
}

const (
	resctrl = "resctrl"

	monDataDirName        = "mon_data"
	monGroupsDirName      = "mon_groups"
	llcOccupancyFileName  = "llc_occupancy"
	mbmLocalBytesFileName = "mbm_local_bytes"
	mbmTotalBytesFileName = "mbm_total_bytes"
)

var (
	rootResctrl          = ""
	enabledMBM           = false
	enabledCMT           = false
	isResctrlInitialized = false
)

type resctrlStatCollector struct {
	logger        *logrus.Logger
	llcOccupancy  *prometheus.Desc
	mbmTotalBytes *prometheus.Desc
	mbmLocalBytes *prometheus.Desc
}

func resctrlCheck() error {
	var err error
	rootResctrl, err = intelrdt.Root()
	if err != nil {
		return fmt.Errorf("unable to initialize resctrl: %v", err)
	}

	enabledMBM = intelrdt.IsMBMEnabled()
	enabledCMT = intelrdt.IsCMTEnabled()
	isResctrlInitialized = true

	return nil
}

func NewResctrlStatCollector(logger *logrus.Logger) (easyxporter.Collector, error) {
	if err := resctrlCheck(); err != nil {
		return nil, err
	}

	if !isResctrlInitialized {
		return nil, errors.New("the resctrl isn't initialized")
	}

	if !(enabledCMT || enabledMBM) {
		return nil, errors.New("there are no monitoring features available")
	}

	dynLabels := []string{"group", "numa"}
	return &resctrlStatCollector{
		llcOccupancy: prometheus.NewDesc(
			prometheus.BuildFQName(resctrl, "llc", "occupancy_bytes"),
			"Last level cache usage statistics counted with RDT Memory Bandwidth Monitoring (MBM).",
			dynLabels, nil,
		),
		mbmTotalBytes: prometheus.NewDesc(
			prometheus.BuildFQName(resctrl, "mem", "bandwidth_total_bytes"),
			"Total memory bandwidth usage statistics counted with RDT Memory Bandwidth Monitoring (MBM).",
			dynLabels, nil,
		),
		mbmLocalBytes: prometheus.NewDesc(
			prometheus.BuildFQName(resctrl, "mem", "bandwidth_local_bytes"),
			"Local memory bandwidth usage statistics counted with RDT Memory Bandwidth Monitoring (MBM).",
			dynLabels, nil,
		),
		logger: logger,
	}, nil
}

func readStatFrom(path string) (uint64, error) {
	context, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	contextString := string(bytes.TrimSpace(context))

	if contextString == "Unavailable" {
		return 0, fmt.Errorf("\"Unavailable\" value from file %q", path)
	}

	stat, err := strconv.ParseUint(contextString, 10, 64)
	if err != nil {
		return stat, fmt.Errorf("unable to parse %q as a uint from file %q", string(context), path)
	}

	return stat, nil
}

// read from one mongroup
func getIntelRDTStatsFrom(path string) (intelrdt.Stats, error) {
	stats := intelrdt.Stats{}

	statsDirectories, err := filepath.Glob(filepath.Join(path, monDataDirName, "*"))
	if err != nil {
		return stats, err
	}

	if len(statsDirectories) == 0 {
		return stats, fmt.Errorf("there is no mon_data stats directories: %q", path)
	}

	var cmtStats []intelrdt.CMTNumaNodeStats
	var mbmStats []intelrdt.MBMNumaNodeStats

	for _, dir := range statsDirectories {
		if enabledCMT {
			llcOccupancy, err := readStatFrom(filepath.Join(dir, llcOccupancyFileName))
			if err != nil {
				return stats, err
			}
			cmtStats = append(cmtStats, intelrdt.CMTNumaNodeStats{LLCOccupancy: llcOccupancy})
		}
		if enabledMBM {
			mbmTotalBytes, err := readStatFrom(filepath.Join(dir, mbmTotalBytesFileName))
			if err != nil {
				return stats, err
			}
			mbmLocalBytes, err := readStatFrom(filepath.Join(dir, mbmLocalBytesFileName))
			if err != nil {
				return stats, err
			}
			mbmStats = append(mbmStats, intelrdt.MBMNumaNodeStats{
				MBMTotalBytes: mbmTotalBytes,
				MBMLocalBytes: mbmLocalBytes,
			})
		}
	}

	stats.CMTStats = &cmtStats
	stats.MBMStats = &mbmStats

	return stats, nil
}

func (c *resctrlStatCollector) Update(ch chan<- prometheus.Metric) error {

	monGroupPaths, err := filepath.Glob(filepath.Join(rootResctrl, monGroupsDirName, "*"))
	if err != nil {
		return err
	}
	monGroupPaths = append(monGroupPaths, rootResctrl)

	for _, mg := range monGroupPaths {
		stats, err := getIntelRDTStatsFrom(mg)
		if err != nil {
			c.logger.Error(err)
			continue
		}

		for i, numaNodeMBMStats := range *stats.MBMStats {
			ch <- prometheus.MustNewConstMetric(
				c.mbmTotalBytes,
				prometheus.CounterValue,
				float64(numaNodeMBMStats.MBMTotalBytes),
				mg, strconv.Itoa(i),
			)
			ch <- prometheus.MustNewConstMetric(
				c.mbmLocalBytes,
				prometheus.CounterValue,
				float64(numaNodeMBMStats.MBMLocalBytes),
				mg, strconv.Itoa(i),
			)
		}

		for i, numaNodeCMTStats := range *stats.CMTStats {
			ch <- prometheus.MustNewConstMetric(
				c.llcOccupancy,
				prometheus.GaugeValue,
				float64(numaNodeCMTStats.LLCOccupancy),
				mg, strconv.Itoa(i),
			)
		}
	}

	return nil
}
