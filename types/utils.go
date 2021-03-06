package types

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/hyperhq/hypercontainer-utils/hlog"
	"github.com/hyperhq/hyperd/utils"
)

func (p *UserPod) LookupContainer(idOrName string) *UserContainer {
	if p == nil {
		return nil
	}
	for _, c := range p.Containers {
		if c.Id == idOrName || c.Name == idOrName {
			return c
		}
	}
	return nil
}

// CloneGlobalPart() clone the static part of a pod spec, and leave the remains
// empty.
func (p *UserPod) CloneGlobalPart() *UserPod {
	return &UserPod{
		Id:            p.Id,
		Hostname:      p.Hostname,
		Type:          p.Type,
		RestartPolicy: p.RestartPolicy,
		Tty:           p.Tty,
		Resource:      p.Resource,
		Log:           p.Log,
		Dns:           p.Dns,
		PortmappingWhiteLists: p.PortmappingWhiteLists,

		Labels:     map[string]string{},
		Containers: []*UserContainer{},
		Files:      []*UserFile{},
		Volumes:    []*UserVolume{},
		Interfaces: []*UserInterface{},
		Services:   []*UserService{},
	}
}

func (p *UserPod) ReorganizeContainers(allowAbsent bool) error {
	if p.Log == nil {
		p.Log = &PodLogConfig{}
	}
	if p.Resource == nil {
		p.Resource = &UserResource{}
	}
	if p.PortmappingWhiteLists == nil {
		p.PortmappingWhiteLists = &PortmappingWhiteList{}
	}
	if p.Hostname == "" {
		p.Hostname = p.Id
	}
	if len(p.Hostname) > 63 {
		p.Hostname = p.Hostname[:63]
	}

	volumes := make(map[string]*UserVolume)
	files := make(map[string]*UserFile)
	for _, vol := range p.Volumes {
		volumes[vol.Name] = vol
	}
	for _, file := range p.Files {
		files[file.Name] = file
	}

	for idx, c := range p.Containers {

		if c.Name == "" {
			_, img, _ := utils.ParseImageRepoTag(c.Image)
			if !utils.IsDNSLabel(img) {
				img = ""
			}

			c.Name = fmt.Sprintf("%s-%s-%d", p.Id, img, idx)
		}

		if p.Tty && !c.Tty {
			c.Tty = true
		}

		cv := []*UserVolumeReference{}
		cf := []*UserFileReference{}

		for _, vol := range c.Volumes {
			if vol.Detail != nil {
				cv = append(cv, vol)
				continue
			}
			if v, ok := volumes[vol.Volume]; !ok {
				if !allowAbsent {
					return fmt.Errorf("volume %s of container %s do not have specification", vol.Volume, c.Name)
				}
				continue
			} else {
				vol.Detail = v
				cv = append(cv, vol)
			}
		}
		for _, file := range c.Files {
			if file.Detail != nil {
				cf = append(cf, file)
				continue
			}
			if f, ok := files[file.Filename]; !ok {
				if !allowAbsent {
					return fmt.Errorf("file %s of container %s do not have specification", file.Filename, c.Name)
				}
				continue
			} else {
				file.Detail = f
				cf = append(cf, file)
			}

		}

		c.Volumes = cv
		c.Files = cf
	}
	return nil
}

type _PortRange struct {
	start int
	end   int
}

func readPortRange(p string) (*_PortRange, error) {
	if p == "" {
		return &_PortRange{1025, 65535}, nil
	} else if strings.Contains(p, "-") {
		parts := strings.SplitN(p, "-", 2)
		start, err := strconv.ParseUint(parts[0], 10, 16)
		if err != nil {
			return nil, err
		}
		end, err := strconv.ParseUint(parts[1], 10, 16)
		if err != nil {
			return nil, err
		}
		if end < start {
			return nil, fmt.Errorf("max %d is smaller than min %d", end, start)
		}
		return &_PortRange{int(start), int(end)}, nil
	}

	start, err := strconv.ParseUint(p, 10, 16)
	if err != nil {
		return nil, err
	}
	return &_PortRange{int(start), int(start)}, nil
}

func (pr *_PortRange) isRange() bool {
	return pr.start != pr.end
}

func (pr *_PortRange) count() int {
	return pr.end - pr.start + 1
}

func (pr *_PortRange) String() string {
	if pr.isRange() {
		return fmt.Sprintf("%d-%d", pr.start, pr.end)
	}
	return fmt.Sprintf("%d", pr.start)
}

type _PortMapping struct {
	host      *_PortRange
	container *_PortRange
	protocol  string
}

func (pm *_PortMapping) String() string {
	return strings.Join([]string{pm.protocol, pm.host.String(), pm.container.String()}, ":")
}

func readPortMapping(pm *PortMapping) (*_PortMapping, error) {
	proto := "tcp"
	if pm.Protocol == "udp" {
		proto = "udp"
	} else if pm.Protocol != "tcp" && pm.Protocol != "" {
		return nil, fmt.Errorf("unrecongnized protocol %s", pm.Protocol)
	}
	h, err := readPortRange(pm.HostPort)
	if err != nil {
		return nil, err
	}
	c, err := readPortRange(pm.ContainerPort)
	if err != nil {
		return nil, err
	}
	if c.isRange() && c.count() != h.count() {
		return nil, fmt.Errorf("port range mismatch: %d vs %d", h.String(), c.String())
	}
	return &_PortMapping{
		host:      h,
		container: c,
		protocol:  proto,
	}, nil
}

func (pm *PortMapping) EqualTo(other *PortMapping) bool {
	if other == nil && pm == nil {
		return true
	} else if other == nil || pm == nil {
		return false
	}
	return pm.Protocol == other.Protocol && pm.ContainerPort == other.ContainerPort && pm.HostPort == other.ContainerPort
}

func (pm *PortMapping) SameDestWith(other *PortMapping) bool {
	if other == nil && pm == nil {
		return true
	} else if other == nil || pm == nil {
		return false
	}
	return pm.Protocol == other.Protocol && pm.ContainerPort == other.ContainerPort
}

func (pm *PortMapping) Formalize() (*PortMapping, error) {
	f, err := readPortMapping(pm)
	if err != nil {
		return nil, err
	}
	return f.toSpec(), nil
}

func (pm *_PortMapping) toSpec() *PortMapping {
	return &PortMapping{
		ContainerPort: pm.container.String(),
		HostPort:      pm.host.String(),
		Protocol:      pm.protocol,
	}
}

func (pm *_PortMapping) isRange() bool {
	return pm.container.isRange() && pm.host.isRange()
}

func (pm *_PortMapping) notDetermined() bool {
	return !pm.container.isRange() && pm.host.isRange()
}

func mergeContinuousPorts(pms []*_PortMapping) ([]*_PortMapping, error) {
	var (
		results = []*_PortMapping{}
		occupy  = map[int]bool{}
		singles = map[int]*_PortMapping{}
		remains = []*_PortMapping{}
		tbm     = []int{}
	)

	for _, pm := range pms {
		if pm.isRange() {
			for i := pm.host.start; i <= pm.host.end; i++ {
				if occupy[i] {
					return nil, fmt.Errorf("duplicate host port %d", i)
				}
				occupy[i] = true
			}
			results = append(results, pm)
			continue
		}
		if pm.notDetermined() {
			remains = append(remains, pm)
			continue
		}
		if occupy[pm.host.start] {
			return nil, fmt.Errorf("duplicate host port %d", pm.host.start)
		}
		occupy[pm.host.start] = true
		singles[pm.host.start] = pm
		tbm = append(tbm, pm.host.start)
	}

	for _, pm := range remains {
		for p := pm.host.start; p <= pm.host.end; p++ {
			if occupy[p] {
				continue
			}
			pm.host.start = p
			pm.host.end = p
			occupy[p] = true
			singles[p] = pm
			tbm = append(tbm, p)
			break
		}
		if pm.notDetermined() {
			return nil, fmt.Errorf("cannot allocate port for %s", pm.host.String())
		}
	}

	sort.Ints(tbm)
	var last *_PortMapping
	for _, p := range tbm {
		cur := singles[p]
		if last != nil {
			if cur.host.start-last.host.end == 1 &&
				cur.container.start-last.container.end == 1 {
				last.host.end++
				last.container.end++
				continue
			} else {
				results = append(results, last)
			}
		}
		last = cur
	}
	if last != nil {
		results = append(results, last)
	}

	return results, nil
}

func (p *UserPod) migrateContainerPorts() error {
	if p.Portmappings == nil {
		p.Portmappings = []*PortMapping{}
	}
	for _, c := range p.Containers {
		if len(c.Ports) == 0 {
			continue
		}
		pms, err := c.ToPodPortmappings(false)
		if err != nil {
			hlog.Log(hlog.ERROR, "failed to convert container port to pod scope: %v", err)
			return err
		}
		p.Portmappings = append(p.Portmappings, pms...)
		c.Ports = nil
	}
	return nil
}

func (p *UserPod) MergePortmappings() error {
	var (
		tcpPorts = []*_PortMapping{}
		udpPorts = []*_PortMapping{}
		err      error
	)

	if err = p.migrateContainerPorts(); err != nil {
		return err
	}

	for _, pm := range p.Portmappings {
		port, err := readPortMapping(pm)
		if err != nil {
			return err
		}
		if port.protocol == "tcp" {
			tcpPorts = append(tcpPorts, port)
		} else if port.protocol == "udp" {
			udpPorts = append(udpPorts, port)
		}
	}

	pms := []*PortMapping{}
	for _, pm := range tcpPorts {
		pms = append(pms, pm.toSpec())
	}
	for _, pm := range udpPorts {
		pms = append(pms, pm.toSpec())
	}
	p.Portmappings = pms

	return nil
}

func (c *UserContainer) ToPodPortmappings(ignoreError bool) ([]*PortMapping, error) {
	result := []*PortMapping{}
	for _, pc := range c.Ports {
		pp := &PortMapping{
			HostPort:      strconv.Itoa(int(pc.HostPort)),
			ContainerPort: strconv.Itoa(int(pc.ContainerPort)),
			Protocol:      strings.ToLower(pc.Protocol),
		}
		if pp.Protocol == "" {
			pp.Protocol = "tcp"
		} else if pp.Protocol != "tcp" || pp.Protocol != "udp" {
			if ignoreError {
				continue
			} else {
				return result, fmt.Errorf("unrecongnized protocol %s", pc.Protocol)
			}
		}
		result = append(result, pp)
	}
	return result, nil
}
