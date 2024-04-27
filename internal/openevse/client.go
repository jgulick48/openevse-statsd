package openevse

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jgulick48/openevse-statsd/internal/metrics"
	"github.com/jgulick48/openevse-statsd/internal/models"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	httpClient *http.Client
	config     models.EVSEConfiguration
	done       chan bool
}

func (c *Client) GetState() (string, error) {
	isEnabled, err := c.IsEnabled()
	if isEnabled {
		return "ON", err
	}
	return "OFF", err
}

func (c *Client) SetState(state string) {
	switch state {
	case "ON":
		c.Enable(true)
	case "OFF":
		c.Enable(false)
	}
	return
}

func (c *Client) InHPState() bool {
	if !c.config.EnableControl {
		return false
	}
	isEnabled, _ := c.IsEnabled()
	return isEnabled
}

func NewClient(config models.EVSEConfiguration, httpClient *http.Client) Client {
	client := Client{
		httpClient: httpClient,
		config:     config,
	}
	ticker := time.NewTicker(10 * time.Second)
	client.done = make(chan bool)
	go func() {
		for {
			select {
			case <-client.done:
				return
			case <-ticker.C:
				if client.config.Enabled {
					client.getAndReportStatus()
				}
			}
		}
	}()
	return client
}

func (c *Client) Stop() {
	c.done <- true
}

func (c *Client) IsEnabled() (bool, error) {
	statusMap, err := c.getStatus()
	if err != nil {
		return false, err
	}
	status, ok := statusMap["status"]
	if !ok {
		return false, errors.New("status was not included in message")
	}
	isEnabled := status == "active"
	return isEnabled, nil
}

func (c *Client) Enable(shouldEnable bool) {
	isEnabled, _ := c.IsEnabled()
	if shouldEnable && !isEnabled {
		c.deleteOverride()
	} else if !shouldEnable && isEnabled {
		c.setOverride()
	}
}

func (c *Client) GetChargeLimitSetting() (int, error) {
	rapi := "$GE"
	result, err := c.processGetRequest(rapi)
	if err != nil {
		return 0, err
	}
	retMessage := strings.Split(result.RET, " ")
	if len(retMessage) == 3 {
		return strconv.Atoi(retMessage[1])
	}
	return 0, fmt.Errorf("did not get expected result got %s", result.RET)
}

func (c *Client) SetChargeLimitSetting(limit int) {
	if limit < 6 {
		limit = 6
	}
	if limit > c.config.MaxChargeCurrent {
		limit = c.config.MaxChargeCurrent
	}
	log.Printf("Setting new charge current limit to %v", limit)
	rapi := fmt.Sprintf("$SC+%v", limit)
	result, err := c.processGetRequest(rapi)
	if err != nil {
		log.Printf("Error setting charge limit setting %s", err)
		return
	}
	retMessage := strings.Split(result.RET, " ")
	if retMessage[0] == "$OK" {
		log.Printf("Updated charging limit with result %s", result.RET)
	}
}

func (c *Client) setOverride() error {
	body := bytes.NewBuffer([]byte("{state: \"disabled\"}"))
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/override", c.config.Address), body)
	var response OverrideResult
	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("Error making request for item from openEVSE: %s", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		log.Printf("Invalid response from openEVSE. Got %v expecting 200", resp.StatusCode)
		return err
	}
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		log.Printf("Unable to decod message from openEVSE: %s", err)
		return err
	}
	return nil
}

func (c *Client) deleteOverride() error {
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/override", c.config.Address), nil)
	var response OverrideResult
	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("Error making request for item from openEVSE: %s", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		log.Printf("Invalid response from openEVSE. Got %v expecting 200", resp.StatusCode)
		return err
	}
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		log.Printf("Unable to decod message from openEVSE: %s", err)
		return err
	}
	return nil
}

func (c *Client) processGetRequest(rapi string) (CommandResult, error) {
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/r?json=1&rapi=%s", c.config.Address, rapi), nil)
	var response CommandResult
	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("Error making request for item from openEVSE: %s", err)
		return response, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("Invalid response from openEVSE. Got %v expecting 200", resp.StatusCode)
		return response, err
	}
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		log.Printf("Unable to decod message from openEVSE: %s", err)
		return response, err
	}
	return response, nil
}

func (c *Client) getAndReportStatus() {
	response, err := c.getStatus()
	if err != nil {
		log.Printf("Unable to decod message from openEVSE: %s", err)
		return
	}
	if metrics.StatsEnabled {
		for key, value := range response {
			switch value.(type) {
			case float64:
				metrics.SendGaugeMetric(fmt.Sprintf("openevse_%s", key), []string{}, value.(float64))
			case bool:
				gaugeValue := 0
				if value.(bool) {
					gaugeValue = 1
				}
				metrics.SendGaugeMetric(fmt.Sprintf("openevse_%s", key), []string{}, float64(gaugeValue))
			case string:
				if key == "time" || key == "local_time" {
					var timeValue time.Time
					if key == "time" {
						timeValue, err = time.Parse("2006-01-02T15:04:05Z", value.(string))
					}
					if key == "local_time" {
						timeValue, err = time.Parse("2006-01-02T15:04:05-0700", value.(string))
					}
					if err != nil {
						log.Printf("Error parsing time of %s: %s\n", value.(string), err)
						continue
					}
					metrics.SendGaugeMetric(fmt.Sprintf("openevse_%s", key), []string{}, float64(timeValue.Unix()))
				} else {
					metrics.SendGaugeMetric(fmt.Sprintf("openevse_%s", key), []string{fmt.Sprintf("%s:%s", key, value.(string))}, float64(1))
				}
			default:
				log.Printf("Got unrecognized type for record %s got %T", key, value)
				continue
			}
		}
	}
	return
}

func (c *Client) getStatus() (map[string]interface{}, error) {
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/status", c.config.Address), nil)
	var response map[string]interface{}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("Error making request for item from openEVSE: %s", err)
		return response, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("Invalid response from openEVSE. Got %v expecting 200", resp.StatusCode)
		return response, nil
	}
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		log.Printf("Unable to decod message from openEVSE: %s", err)
		return response, err
	}
	log.Println("Got new status from openEvse")
	return response, nil
}
