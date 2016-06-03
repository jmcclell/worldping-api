package v1

import (
	"fmt"
	"time"

	"github.com/grafana/grafana/pkg/log"
	"github.com/raintank/worldping-api/pkg/middleware"
	m "github.com/raintank/worldping-api/pkg/models"
	"github.com/raintank/worldping-api/pkg/services/sqlstore"
)

func GetMonitors(c *middleware.Context, query m.GetMonitorsQuery) {
	endpoint, err := sqlstore.GetEndpointById(c.OrgId, query.EndpointId)
	if err != nil {
		c.JSON(500, fmt.Sprintf("Failed to list monitors. %s", err))
		return
	}
	if endpoint == nil {
		c.JSON(200, []m.MonitorDTO{})
	}

	monitors := make([]m.MonitorDTO, len(endpoint.Checks))
	for i, check := range endpoint.Checks {
		monitors[i] = m.MonitorDTOFromCheck(check, endpoint.Slug)
		probeList, err := sqlstore.GetProbesForCheck(&check)
		if err != nil {
			c.JSON(500, fmt.Sprintf("Failed to list probes for check. %s", err))
			return
		}
		monitors[i].Collectors = probeList
	}
	c.JSON(200, monitors)
}

func GetMonitorTypes(c *middleware.Context) {
	c.JSON(200, m.MonitorTypes)
}

func DeleteMonitor(c *middleware.Context) {
	id := c.ParamsInt64(":id")

	check, err := sqlstore.GetCheckById(c.OrgId, id)
	if err != nil {
		if err.Error() == "check not found" {
			c.JSON(404, "check not found")
		} else {
			c.JSON(500, fmt.Sprintf("Failed to query DB. %s", err))
		}
		return
	}

	// get the endpoint that the check belongs too.
	endpoint, err := sqlstore.GetEndpointById(c.OrgId, check.EndpointId)
	if err != nil {
		c.JSON(500, fmt.Sprintf("failed to query DB. %s", err))
		return
	}

	// now update the endpoint and remove the check.
	newChecks := make([]m.Check, 0)
	for _, ch := range endpoint.Checks {
		if ch.Id != id {
			newChecks = append(newChecks, ch)
		}
	}
	endpoint.Checks = newChecks
	err = sqlstore.UpdateEndpoint(endpoint)
	if err != nil {
		c.JSON(500, fmt.Sprintf("failed remove check from endpoint. %s", err))
		return
	}
	c.JSON(200, "monitor deleted")
}

func AddMonitor(c *middleware.Context, cmd m.AddMonitorCommand) {
	cmd.OrgId = c.OrgId
	if cmd.EndpointId == 0 {
		c.JSON(400, "EndpointId not set.")
		return
	}
	if cmd.MonitorTypeId == 0 {
		c.JSON(400, "MonitorTypeId not set.")
		return
	}
	if cmd.MonitorTypeId > 4 {
		c.JSON(400, "Invlaid MonitorTypeId.")
		return
	}
	if cmd.Frequency == 0 {
		c.JSON(400, "Frequency not set.")
		return
	}

	// get the endpoint that the check belongs too.
	endpoint, err := sqlstore.GetEndpointById(c.OrgId, cmd.EndpointId)
	if err != nil {
		log.Error(3, "failed to add get endpoint for check. %s", err)
		c.JSON(500, fmt.Sprintf("failed to add get endpoint for check %s", err))
		return
	}
	if endpoint == nil {
		c.JSON(400, "endpoing does not exist.")
		return
	}

	for _, check := range endpoint.Checks {
		if checkTypeToId(check.Type) == cmd.MonitorTypeId {
			c.JSON(400, fmt.Sprintf("Endpoint already has a %s check.", check.Type))
			return
		}
	}

	route := &m.CheckRoute{}
	if len(cmd.CollectorTags) > 0 {
		route.Type = m.RouteByTags
		route.Config = map[string]interface{}{
			"tags": cmd.CollectorTags,
		}
	} else {
		route.Type = m.RouteByIds
		route.Config = map[string]interface{}{
			"ids": cmd.CollectorIds,
		}
	}

	endpoint.Checks = append(endpoint.Checks, m.Check{
		OrgId:          cmd.OrgId,
		EndpointId:     cmd.EndpointId,
		Type:           m.MonitorTypeToCheckTypeMap[cmd.MonitorTypeId-1],
		Frequency:      cmd.Frequency,
		Enabled:        cmd.Enabled,
		HealthSettings: cmd.HealthSettings,
		Created:        time.Now(),
		Updated:        time.Now(),
		Route:          route,
		Settings:       m.MonitorSettingsDTO(cmd.Settings).ToV2Setting(m.MonitorTypeToCheckTypeMap[cmd.MonitorTypeId-1]),
	})

	//Update endpoint
	err = sqlstore.UpdateEndpoint(endpoint)
	if err != nil {
		log.Error(3, "failed to add check to endpoint. %s", err)
		c.JSON(500, fmt.Sprintf("failed to add check to endpoint. %s", err))
	}

	var monitor m.MonitorDTO
	for _, check := range endpoint.Checks {
		if check.Type == m.MonitorTypeToCheckTypeMap[cmd.MonitorTypeId-1] {
			monitor = m.MonitorDTOFromCheck(check, endpoint.Slug)
			break
		}
	}

	c.JSON(200, monitor)
	return
}

func UpdateMonitor(c *middleware.Context, cmd m.UpdateMonitorCommand) {
	cmd.OrgId = c.OrgId
	if cmd.EndpointId == 0 {
		c.JSON(400, "EndpointId not set.")
		return
	}
	if cmd.MonitorTypeId == 0 {
		c.JSON(400, "MonitorTypeId not set.")
		return
	}
	if cmd.MonitorTypeId > 4 {
		c.JSON(400, "Invlaid MonitorTypeId.")
		return
	}
	if cmd.Frequency == 0 {
		c.JSON(400, "Frequency not set.")
		return
	}

	// get the endpoint that the check belongs too.
	endpoint, err := sqlstore.GetEndpointById(c.OrgId, cmd.EndpointId)
	if err != nil {
		log.Error(3, "failed to get endpoint for check. %s", err)
		c.JSON(500, fmt.Sprintf("failed to get endpoint for check %s", err))
		return
	}
	if endpoint == nil {
		c.JSON(400, "endpoint does not exist.")
		return
	}
	route := &m.CheckRoute{}
	if len(cmd.CollectorTags) > 0 {
		route.Type = m.RouteByTags
		route.Config = map[string]interface{}{
			"tags": cmd.CollectorTags,
		}
	} else {
		route.Type = m.RouteByIds
		route.Config = map[string]interface{}{
			"ids": cmd.CollectorIds,
		}
	}
	checkPos := 0
	found := false
	for pos, check := range endpoint.Checks {
		if check.Id == cmd.Id {
			checkPos = pos
			found = true
			log.Debug("updating check %d of endpoint %s", check.Id, endpoint.Slug)
			if check.Type != m.MonitorTypeToCheckTypeMap[cmd.MonitorTypeId-1] {
				c.JSON(400, "monitor Type cant be changed.")
				return
			}
			break
		}
	}
	if !found {
		c.JSON(400, "check does not exist in endpoint.")
		return
	}
	endpoint.Checks[checkPos].Frequency = cmd.Frequency
	endpoint.Checks[checkPos].Enabled = cmd.Enabled
	endpoint.Checks[checkPos].HealthSettings = cmd.HealthSettings
	endpoint.Checks[checkPos].Updated = time.Now()
	endpoint.Checks[checkPos].Route = route
	endpoint.Checks[checkPos].Settings = m.MonitorSettingsDTO(cmd.Settings).ToV2Setting(m.MonitorTypeToCheckTypeMap[cmd.MonitorTypeId-1])

	err = sqlstore.UpdateEndpoint(endpoint)
	if err != nil {
		log.Error(3, "Failed to update monitor. %s", err)
		c.JSON(500, fmt.Sprintf("Failed to update monitor. %s", err))
		return
	}

	c.JSON(200, "Monitor updated")
}
