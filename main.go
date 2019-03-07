package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	cb "github.com/clearblade/Go-SDK"
	mqttTypes "github.com/clearblade/mqtt_parsing"
	mqtt "github.com/clearblade/paho.mqtt.golang"
	"github.com/fsnotify/fsnotify"
	"github.com/hashicorp/logutils"
)

const (
	platURL             = "http://localhost:9000"
	messURL             = "localhost:1883"
	msgSubscribeQos     = 0
	msgPublishQos       = 0
	JavascriptISOString = "2006-01-02T15:04:05.000Z07:00"

	CONDUIT_PRODUCT_ID_PREFIX = "MTCDT"
	MTACGPIO_PRODUCT_ID       = "MTAC-GPIOB"

	//MQTT topic strings
	gpioRead  = "read"
	gpioWrite = "write"

	//mtsio command
	MTSIO_CMD = "mts-io-sysfs"

	//mtsio command operations
	MTSIO_READ  = "show"
	MTSIO_WRITE = "store"

	//
	DIGITAL_INPUT_PREFIX  = "din"
	DIGITAL_OUTPUT_PREFIX = "dout"
	ANALOG_INPUT_PREFIX   = "adc"

	//mtsio boolean values
	MTSIO_ON  = 0
	MTSIO_OFF = 1

	GPIO_DIRECTORY = "/sys/devices/platform/mts-io"
)

var (
	platformURL         string //Defaults to http://localhost:9000
	messagingURL        string //Defaults to localhost:1883
	sysKey              string
	sysSec              string
	deviceName          string //Defaults to mtacGpioAdapter
	activeKey           string
	logLevel            string //Defaults to info
	adapterConfigCollID string
	gpioDataCollID      string

	topicRoot                 = "wayside/gpio"
	serialPortName            = ""
	cbBroker                  cbPlatformBroker
	cbSubscribeChannel        <-chan *mqttTypes.Publish
	endSubscribeWorkerChannel chan string
	endFileWatcherChannel     chan string

	currentValues = map[string]interface{}{
		"digital": map[string]interface{}{
			"input": map[string]interface{}{
				"StartAddress": 0,
				"AddressCount": 4,
				"Data":         []float64{0, 0, 0, 0},
			},
			"output": map[string]interface{}{
				"StartAddress": 0,
				"AddressCount": 4,
				"Data":         []float64{0, 0, 0, 0},
			},
		},
		"analog": map[string]interface{}{
			"input": map[string]interface{}{
				"StartAddress": 0,
				"AddressCount": 3,
				"Data":         []float64{0, 0, 0},
			},
		},
	}

	activeCycles = map[string]chan bool{}
)

type cbPlatformBroker struct {
	name         string
	clientID     string
	client       *cb.DeviceClient
	platformURL  *string
	messagingURL *string
	systemKey    *string
	systemSecret *string
	username     *string
	password     *string
	topic        string
	qos          int
}

func init() {
	flag.StringVar(&sysKey, "systemKey", "", "system key (required)")
	flag.StringVar(&sysSec, "systemSecret", "", "system secret (required)")
	flag.StringVar(&deviceName, "deviceName", "mtacGpioAdapter", "name of device (optional)")
	flag.StringVar(&activeKey, "password", "", "password (or active key) for device authentication (required)")
	flag.StringVar(&platformURL, "platformURL", platURL, "platform url (optional)")
	flag.StringVar(&messagingURL, "messagingURL", messURL, "messaging URL (optional)")
	flag.StringVar(&logLevel, "logLevel", "info", "The level of logging to use. Available levels are 'debug, 'info', 'warn', 'error', 'fatal' (optional)")

	flag.StringVar(&adapterConfigCollID, "adapterConfigCollectionID", "", "The ID of the data collection used to house adapter configuration (required)")
	flag.StringVar(&gpioDataCollID, "gpioDataCollectionID", "", "Collection ID to store current GPIO values - if provided updates to GPIO values will be made in the collection as well as published via MQTT (optional)")

}

func usage() {
	log.Printf("Usage: mtacGpioAdapter [options]\n\n")
	flag.PrintDefaults()
}

func validateFlags() {
	flag.Parse()

	if sysKey == "" || sysSec == "" || activeKey == "" || adapterConfigCollID == "" {

		log.Printf("ERROR - Missing required flags\n\n")
		flag.Usage()
		os.Exit(1)
	}
}

func main() {
	fmt.Println("Starting mtacGpioAdapter...")

	//Validate the command line flags
	flag.Usage = usage
	validateFlags()

	//create the log file with the correct permissions
	logfile, err := os.OpenFile("/var/log/mtacGpioAdapter", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		log.Fatal(err)
	}

	defer logfile.Close()

	//Initialize the logging mechanism
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	filter := &logutils.LevelFilter{
		Levels:   []logutils.LogLevel{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"},
		MinLevel: logutils.LogLevel(strings.ToUpper(logLevel)),
		Writer:   logfile,
	}
	log.SetOutput(filter)

	cbBroker = cbPlatformBroker{

		name:         "ClearBlade",
		clientID:     deviceName + "_client",
		client:       nil,
		platformURL:  &platformURL,
		messagingURL: &messagingURL,
		systemKey:    &sysKey,
		systemSecret: &sysSec,
		username:     &deviceName,
		password:     &activeKey,
		qos:          msgSubscribeQos,
	}

	log.Println("[DEBUG] main - Determining MTAC-GPIO port name")
	setSerialPortName()

	if serialPortName == "" {
		log.Fatalf("[FATAL] Unable to detect MTAC-GPIO on AP1 or AP2")
		panic("Unable to detect MTAC-GPIO on AP1 or AP2")
	}

	log.Printf("[DEBUG] current values = %#v\n", currentValues)

	// Initialize ClearBlade Client
	if err = initCbClient(cbBroker); err != nil {
		log.Println(err.Error())
		log.Println("Unable to initialize CB broker client. Exiting.")
		return
	}

	initGpioCollection()

	//Read the current GPIO values
	for i := 0; i < 4; i++ {
		if val, err := executeMtsioCommand(MTSIO_READ, fmt.Sprintf("%s%d", DIGITAL_INPUT_PREFIX, i), nil); err == nil {
			currentValues["digital"].(map[string]interface{})["input"].(map[string]interface{})["Data"].([]float64)[i] = val.(float64)
			storeGpioValueInCollection(DIGITAL_INPUT_PREFIX+strconv.Itoa(i), val.(float64))
		}
		if val, err := executeMtsioCommand(MTSIO_READ, fmt.Sprintf("%s%d", DIGITAL_OUTPUT_PREFIX, i), nil); err == nil {
			currentValues["digital"].(map[string]interface{})["output"].(map[string]interface{})["Data"].([]float64)[i] = val.(float64)
			storeGpioValueInCollection(DIGITAL_OUTPUT_PREFIX+strconv.Itoa(i), val.(float64))
		}
		if i < 3 {
			if val, err := executeMtsioCommand(MTSIO_READ, fmt.Sprintf("%s%d", ANALOG_INPUT_PREFIX, i), nil); err == nil {
				currentValues["analog"].(map[string]interface{})["input"].(map[string]interface{})["Data"].([]float64)[i] = val.(float64)
				storeGpioValueInCollection(ANALOG_INPUT_PREFIX+strconv.Itoa(i), val.(float64))
			}
		}
	}

	defer close(endSubscribeWorkerChannel)
	defer close(endFileWatcherChannel)
	endSubscribeWorkerChannel = make(chan string)
	endFileWatcherChannel = make(chan string)

	//Handle OS interrupts to shut down gracefully
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	sig := <-c

	log.Printf("[INFO] OS signal %s received, ending go routines.", sig)

	//End the existing goRoutines
	endSubscribeWorkerChannel <- "Stop Channel"
	endFileWatcherChannel <- "Stop Channel"
	os.Exit(0)
}

// ClearBlade Client init helper
func initCbClient(platformBroker cbPlatformBroker) error {
	log.Println("[DEBUG] initCbClient - Initializing the ClearBlade client")

	cbBroker.client = cb.NewDeviceClientWithAddrs(*(platformBroker.platformURL), *(platformBroker.messagingURL), *(platformBroker.systemKey), *(platformBroker.systemSecret), *(platformBroker.username), *(platformBroker.password))

	for err := cbBroker.client.Authenticate(); err != nil; {
		log.Printf("[ERROR] initCbClient - Error authenticating %s: %s\n", platformBroker.name, err.Error())
		log.Println("[ERROR] initCbClient - Will retry in 1 minute...")

		// sleep 1 minute
		time.Sleep(time.Duration(time.Minute * 1))
		err = cbBroker.client.Authenticate()
	}

	//Retrieve adapter configuration data
	log.Println("[INFO] main - Retrieving adapter configuration...")
	getAdapterConfig()

	log.Println("[DEBUG] initCbClient - Initializing MQTT")
	callbacks := cb.Callbacks{OnConnectionLostCallback: OnConnectLost, OnConnectCallback: OnConnect}
	if err := cbBroker.client.InitializeMQTTWithCallback(platformBroker.clientID, "", 30, nil, nil, &callbacks); err != nil {
		log.Fatalf("[FATAL] initCbClient - Unable to initialize MQTT connection with %s: %s", platformBroker.name, err.Error())
		return err
	}

	return nil
}

//If the connection to the broker is lost, we need to reconnect and
//re-establish all of the subscriptions
func OnConnectLost(client mqtt.Client, connerr error) {
	log.Printf("[INFO] OnConnectLost - Connection to broker was lost: %s\n", connerr.Error())

	//End the existing goRoutines
	endSubscribeWorkerChannel <- "Stop Channel"
	endFileWatcherChannel <- "Stop Channel"

	//We don't need to worry about manally re-initializing the mqtt client. The auto reconnect logic will
	//automatically try and reconnect. The reconnect interval could be as much as 20 minutes.
}

//When the connection to the broker is complete, set up the subscriptions
func OnConnect(client mqtt.Client) {
	log.Println("[INFO] OnConnect - Connected to ClearBlade Platform MQTT broker")

	//CleanSession, by default, is set to true. This results in non-durable subscriptions.
	//We therefore need to re-subscribe
	log.Println("[DEBUG] OnConnect - Begin Configuring Subscription(s)")

	var err error
	for cbSubscribeChannel, err = subscribe(topicRoot + "/request"); err != nil; {
		//Wait 30 seconds and retry
		log.Printf("[ERROR] OnConnect - Error subscribing to MQTT: %s\n", err.Error())
		log.Println("[ERROR] OnConnect - Will retry in 30 seconds...")
		time.Sleep(time.Duration(30 * time.Second))
		cbSubscribeChannel, err = subscribe(topicRoot + "/request")
	}

	//Publish the initial values
	jsonPayload := make(map[string]interface{})
	jsonPayload["action"] = gpioRead
	jsonPayload["success"] = true
	jsonPayload["gpio"] = currentValues

	publishGpioResponse(jsonPayload)

	//Set up file listeners
	go watchFiles()

	//Start subscribe worker
	go subscribeWorker()
}

func subscribeWorker() {
	log.Println("[DEBUG] subscribeWorker - Starting subscribeWorker")

	//Wait for subscriptions to be received
	for {
		select {
		case message, ok := <-cbSubscribeChannel:
			if ok {
				handleRequest(message.Payload)
			}
		case _ = <-endSubscribeWorkerChannel:
			//End the current go routine when the stop signal is received
			log.Println("[INFO] subscribeWorker - Stopping subscribeWorker")
			return
		}
	}
}

func handleRequest(payload []byte) {
	// The json request should resemble the following:
	// {
	// 		“action”: “read”,
	// 		"gpio": {
	// 			"digital": {
	// 				"input": {
	// 					“StartAddress”: 0,
	// 					“AddressCount”: 4
	// 				},
	// 				"output": {
	// 					“StartAddress”: 0,
	// 					“AddressCount”: 4
	// 				},
	// 			},
	// 			"analog": {
	// 				"input": {
	// 					“StartAddress”: 0,
	// 					“AddressCount”: 4
	// 				},
	// 				"output": {
	// 					“StartAddress”: 0,
	// 					“AddressCount”: 4
	// 				},
	// 			},
	// 		}
	// }

	// or this for a cycle request:
	// {
	// 	"action": "cycle_start" / "cycle_end",
	// 	"on_interval": 5, (only required on cycle_start action)
	// 	"off_interval": 3, (only required on cycle_start action)
	// 	"gpio": {
	// 		"digital": {
	// 			"output": {
	// 				"Address": 0
	// 			}
	// 		}
	// 	}
	// }

	log.Printf("[DEBUG] handleRequest - Json payload received: %s\n", string(payload))

	var jsonPayload map[string]interface{}

	if err := json.Unmarshal(payload, &jsonPayload); err != nil {
		log.Printf("[ERROR] handleRequest - Error encountered unmarshalling json: %s\n", err.Error())
		addErrorToPayload(jsonPayload, "Error encountered unmarshalling json: "+err.Error())
		jsonPayload["request"] = payload
	} else {
		log.Printf("[DEBUG] handleRequest - Json payload received: %#v\n", jsonPayload)
	}

	if jsonPayload["action"] == nil {
		log.Println("[ERROR] handleRequest - action object not specified in incoming payload")
		addErrorToPayload(jsonPayload, "The action object is required")
		jsonPayload["request"] = payload
	}

	if jsonPayload["gpio"] == nil {
		log.Println("[ERROR] handleRequest - gpio object not specified in incoming payload")
		addErrorToPayload(jsonPayload, "The gpio object is required")
		jsonPayload["request"] = payload
	}

	if jsonPayload["error"] == nil {
		var err error
		if jsonPayload["action"].(string) == gpioRead {
			err = readGpioValues(jsonPayload["gpio"].(map[string]interface{}))
		} else if jsonPayload["action"].(string) == MTSIO_WRITE {
			err = writeGpioValues(jsonPayload["gpio"].(map[string]interface{}))
		} else if jsonPayload["action"].(string) == "cycle_start" {
			err = startGpioCycle(jsonPayload["gpio"].(map[string]interface{}), jsonPayload["on_interval"].(float64), jsonPayload["off_interval"].(float64))
		} else if jsonPayload["action"].(string) == "cycle_end" {
			err = stopGpioCycle(jsonPayload["gpio"].(map[string]interface{}))
		}

		log.Printf("[DEBUG] handleRequest - err = %#v\n", err)
		log.Printf("[DEBUG] handleRequest - jsonPayload = %#v\n", jsonPayload)

		if err != nil {
			addErrorToPayload(jsonPayload, err.Error())
		} else {
			if jsonPayload["success"] == nil {
				jsonPayload["success"] = true
			}
		}
	}

	publishGpioResponse(jsonPayload)
}

func startGpioCycle(gpio map[string]interface{}, onInterval, offInterval float64) error {

	var gpioAddress int
	if gpio["digital"] != nil {
		if gpio["digital"].(map[string]interface{})["output"] != nil {
			gpioAddress = int(gpio["digital"].(map[string]interface{})["output"].(map[string]interface{})["Address"].(float64))
		} else {
			return errors.New("missing gpio.digital.output in cycle request (only gpio type cycle is valid for)")
		}
	} else {
		return errors.New("missing gpio.digital in cycle request")
	}

	gpioKey := DIGITAL_OUTPUT_PREFIX + strconv.Itoa(gpioAddress)

	// first check if cycle already exists for this address, if so don't start a new one
	if _, ok := activeCycles[gpioKey]; ok {
		return errors.New("this digital input is already currently cycling, end current cycle before starting a new one")
	}

	quit := make(chan bool)
	activeCycles[gpioKey] = quit
	go cycleGPIO(gpioKey, quit, onInterval, offInterval)
	return nil
}

func cycleGPIO(gpioToCycle string, quit chan bool, onInterval, offInterval float64) {
	//probably a more elegant solution for this
	totalInterval := onInterval + offInterval
	if _, err := executeMtsioCommand(MTSIO_WRITE, gpioToCycle, MTSIO_ON); err != nil {
		log.Printf("[ERROR] cycleGPIO - failed to turn on GPIO: %s\n", err.Error())
	}
	onTicker := time.NewTicker(time.Duration(totalInterval) * time.Second)
	defer onTicker.Stop()
	time.Sleep(time.Duration(onInterval) * time.Second)
	offTicker := time.NewTicker(time.Duration(totalInterval) * time.Second)
	defer offTicker.Stop()
	for {
		select {
		case <-quit:
			if _, err := executeMtsioCommand(MTSIO_WRITE, gpioToCycle, MTSIO_OFF); err != nil {
				log.Printf("[ERROR] cycleGPIO - failed to turn off GPIO: %s\n", err.Error())
			}
			return
		case <-onTicker.C:
			if _, err := executeMtsioCommand(MTSIO_WRITE, gpioToCycle, MTSIO_OFF); err != nil {
				log.Printf("[ERROR] cycleGPIO - failed to turn off GPIO: %s\n", err.Error())
			}
			break
		case <-offTicker.C:
			if _, err := executeMtsioCommand(MTSIO_WRITE, gpioToCycle, MTSIO_ON); err != nil {
				log.Printf("[ERROR] cycleGPIO - failed to turn on GPIO: %s\n", err.Error())
			}
			break
		}
	}
}

func stopGpioCycle(gpio map[string]interface{}) error {
	var gpioAddress int
	if gpio["digital"] != nil {
		if gpio["digital"].(map[string]interface{})["output"] != nil {
			gpioAddress = int(gpio["digital"].(map[string]interface{})["output"].(map[string]interface{})["Address"].(float64))
		} else {
			return errors.New("missing gpio.digital.output in stop cycle request (only gpio type cycle is valid for)")
		}
	} else {
		return errors.New("missing gpio.digital in stop cycle request")
	}
	if theChan, ok := activeCycles[DIGITAL_OUTPUT_PREFIX+strconv.Itoa(gpioAddress)]; ok {
		theChan <- true
	} else {
		return errors.New("provided GPIO is not currently cycling")
	}
	delete(activeCycles, DIGITAL_OUTPUT_PREFIX+strconv.Itoa(gpioAddress))
	return nil
}

func readGpioValues(gpio map[string]interface{}) error {
	// 		"gpio": {
	// 			"digital": {
	// 				"input": {
	// 					“StartAddress”: 0,
	// 					“AddressCount”: 4
	// 				},
	// 				"output": {
	// 					“StartAddress”: 0,
	// 					“AddressCount”: 4
	// 				},
	// 			},
	// 			"analog": {
	// 				"input": {
	// 					“StartAddress”: 0,
	// 					“AddressCount”: 4
	// 				},
	// 				"output": {
	// 					“StartAddress”: 0,
	// 					“AddressCount”: 4
	// 				},
	// 			},
	// 		}

	log.Printf("[DEBUG] current values = %#v\n", currentValues)

	if gpio["digital"] != nil {
		if gpio["digital"].(map[string]interface{})["input"] != nil {
			if gpio["digital"].(map[string]interface{})["input"].(map[string]interface{})["StartAddress"] == nil {
				return errors.New("StartAddress missing in digital.input")
			}

			if gpio["digital"].(map[string]interface{})["input"].(map[string]interface{})["AddressCount"] == nil {
				return errors.New("AddressCount missing in digital.input")
			}

			startAddr := int(gpio["digital"].(map[string]interface{})["input"].(map[string]interface{})["StartAddress"].(float64))
			addrCount := int(gpio["digital"].(map[string]interface{})["input"].(map[string]interface{})["AddressCount"].(float64))
			data := currentValues["digital"].(map[string]interface{})["input"].(map[string]interface{})["Data"].([]float64)

			gpio["digital"].(map[string]interface{})["input"].(map[string]interface{})["Data"] = data[startAddr : startAddr+addrCount]

			log.Printf("[DEBUG] gpio = %#v\n", gpio)
		}

		if gpio["digital"].(map[string]interface{})["output"] != nil {
			if gpio["digital"].(map[string]interface{})["output"].(map[string]interface{})["StartAddress"] == nil {
				return errors.New("StartAddress missing in digital.output")
			}

			if gpio["digital"].(map[string]interface{})["output"].(map[string]interface{})["AddressCount"] == nil {
				return errors.New("AddressCount missing in digital.output")
			}

			startAddr := int(gpio["digital"].(map[string]interface{})["output"].(map[string]interface{})["StartAddress"].(float64))
			addrCount := int(gpio["digital"].(map[string]interface{})["output"].(map[string]interface{})["AddressCount"].(float64))
			data := currentValues["digital"].(map[string]interface{})["output"].(map[string]interface{})["Data"].([]float64)

			gpio["digital"].(map[string]interface{})["output"].(map[string]interface{})["Data"] = data[startAddr : startAddr+addrCount]

			log.Printf("[DEBUG] gpio = %#v\n", gpio)
		}
	}

	if gpio["analog"] != nil {
		if gpio["analog"].(map[string]interface{})["input"] != nil {
			if gpio["analog"].(map[string]interface{})["input"].(map[string]interface{})["StartAddress"] == nil {
				return errors.New("StartAddress missing in analog.input")
			}

			if gpio["analog"].(map[string]interface{})["input"].(map[string]interface{})["AddressCount"] == nil {
				return errors.New("AddressCount missing in analog.input")
			}

			startAddr := int(gpio["analog"].(map[string]interface{})["input"].(map[string]interface{})["StartAddress"].(float64))
			addrCount := int(gpio["analog"].(map[string]interface{})["input"].(map[string]interface{})["AddressCount"].(float64))
			data := currentValues["analog"].(map[string]interface{})["input"].(map[string]interface{})["Data"].([]float64)

			gpio["analog"].(map[string]interface{})["input"].(map[string]interface{})["Data"] = data[startAddr : startAddr+addrCount]

			log.Printf("[DEBUG] gpio = %#v\n", gpio)
		}
	}
	return nil
}

func writeGpioValues(gpio map[string]interface{}) error {
	if gpio["digital"] != nil {
		if gpio["digital"].(map[string]interface{})["output"] != nil {
			if gpio["digital"].(map[string]interface{})["output"].(map[string]interface{})["StartAddress"] == nil {
				return errors.New("StartAddress missing in digital.output")
			}

			if gpio["digital"].(map[string]interface{})["output"].(map[string]interface{})["AddressCount"] == nil {
				return errors.New("AddressCount missing in digital.output")
			}

			if gpio["digital"].(map[string]interface{})["output"].(map[string]interface{})["Data"] == nil {
				return errors.New("Data array missing in digital.output write request")
			}

			data := gpio["digital"].(map[string]interface{})["output"].(map[string]interface{})["Data"].([]interface{})

			err := writeValues(MTSIO_WRITE,
				DIGITAL_OUTPUT_PREFIX,
				int(gpio["digital"].(map[string]interface{})["output"].(map[string]interface{})["StartAddress"].(float64)),
				int(gpio["digital"].(map[string]interface{})["output"].(map[string]interface{})["AddressCount"].(float64)),
				data)

			if err != nil {
				return err
			}
		} else {
			return errors.New("digital.output object is missing")
		}
	} else {
		return errors.New("digital object is missing")
	}
	return nil
}

func writeValues(action string, gpioPrefix string, startAddress int, addressCount int, data []interface{}) error {
	log.Println("[DEBUG] writeValues - writing values")
	for i := 0; i < addressCount; i++ {
		if _, err := executeMtsioCommand(action, fmt.Sprintf("%s%d", gpioPrefix, i+startAddress), data[i].(float64)); err == nil {
			currentValues["digital"].(map[string]interface{})["output"].(map[string]interface{})["Data"].([]float64)[i+startAddress] = data[i].(float64)
		} else {
			return err
		}
	}
	return nil
}

func createCommandArgs(operation string, portName string, ioPort string, value interface{}) []string {
	var cmdArgs = []string{operation, portName + "/" + ioPort}

	//Only append values to the command on write requests
	if operation == MTSIO_WRITE {
		if value != nil {
			switch value.(type) {
			case string:
				cmdArgs = append(cmdArgs, value.(string))
			case float64:
				//cmdArgs = append(cmdArgs, strconv.FormatFloat(value.(float64), 'E', -1, 64))
				cmdArgs = append(cmdArgs, strconv.Itoa(int(value.(float64))))
			case bool:
				if value.(bool) {
					cmdArgs = append(cmdArgs, strconv.Itoa(MTSIO_ON))
				} else {
					cmdArgs = append(cmdArgs, strconv.Itoa(MTSIO_OFF))
				}
			case int:
				cmdArgs = append(cmdArgs, strconv.Itoa(value.(int)))
			}

		}
	}
	return cmdArgs
}

func executeMtsioCommand(operation string, ioPort string, value interface{}) (interface{}, error) {
	log.Printf("[DEBUG] executeMtsioCommand - Executing command: %#v\n", createCommandArgs(operation, serialPortName, ioPort, value))
	cmd := exec.Command(MTSIO_CMD, createCommandArgs(operation, serialPortName, ioPort, value)...)
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		log.Printf("[ERROR] executeMtsioCommand - ERROR executing mts-io-sysfs command: %s\n", err.Error())
		return nil, err
	} else {
		log.Printf("[DEBUG] executeMtsioCommand - Command response received: %s\n", out.String())
		if operation == MTSIO_READ {
			return convertResponseValue(strings.Replace(out.String(), "\n", "", -1)), nil
		}
		return nil, nil
	}
}

func convertResponseValue(respVal string) interface{} {
	if numVal, err := strconv.ParseInt(respVal, 10, 32); err == nil {
		return float64(numVal)
	} else {
		return respVal
	}
}

func addErrorToPayload(payload map[string]interface{}, errMsg string) {
	payload["success"] = false
	payload["error"] = errMsg
}

// Subscribes to a topic
func subscribe(topic string) (<-chan *mqttTypes.Publish, error) {
	log.Printf("[DEBUG] subscribe - Subscribing to topic %s\n", topic)
	subscription, error := cbBroker.client.Subscribe(topic, cbBroker.qos)
	if error != nil {
		log.Printf("[ERROR] subscribe - Unable to subscribe to topic: %s due to error: %s\n", topic, error.Error())
		return nil, error
	}

	log.Printf("[DEBUG] subscribe - Successfully subscribed to = %s\n", topic)
	return subscription, nil
}

// Publishes data to a topic
func publish(topic string, data string) error {
	log.Printf("[DEBUG] publish - Publishing to topic %s\n", topic)
	error := cbBroker.client.Publish(topic, []byte(data), cbBroker.qos)
	if error != nil {
		log.Printf("[ERROR] publish - Unable to publish to topic: %s due to error: %s\n", topic, error.Error())
		return error
	}

	log.Printf("[DEBUG] publish - Successfully published message to = %s\n", topic)
	return nil
}

func getAdapterConfig() {
	log.Println("[INFO] getAdapterConfig - Retrieving adapter config")

	//Retrieve the adapter configuration row
	query := cb.NewQuery()
	query.EqualTo("adapter_name", "mtacGpioAdapter")

	//A nil query results in all rows being returned
	log.Println("[DEBUG] getAdapterConfig - Executing query against table " + adapterConfigCollID)
	results, err := cbBroker.client.GetData(adapterConfigCollID, query)
	if err != nil {
		log.Println("[DEBUG] getAdapterConfig - Adapter configuration could not be retrieved. Using defaults")
		log.Printf("[DEBUG] getAdapterConfig - Error: %s\n", err.Error())
	} else {
		if len(results["DATA"].([]interface{})) > 0 {
			log.Printf("[DEBUG] getAdapterConfig - Adapter config retrieved: %#v\n", results)
			log.Println("[INFO] getAdapterConfig - Adapter config retrieved")

			//topic root
			if results["DATA"].([]interface{})[0].(map[string]interface{})["topic_root"] != nil {
				log.Printf("[DEBUG] getAdapterConfig - Setting topicRoot to %s\n", results["DATA"].([]interface{})[0].(map[string]interface{})["topic_root"].(string))
				topicRoot = results["DATA"].([]interface{})[0].(map[string]interface{})["topic_root"].(string)
			} else {
				log.Printf("[DEBUG] getAdapterConfig - Topic root is nil. Using default value %s\n", topicRoot)
			}
		} else {
			log.Println("[DEBUG] getAdapterConfig - No rows returned. Using defaults")
		}
	}
}

func setSerialPortName() {
	// 1. Determine if we are running on a Multitech Conduit
	productId := getProductId("")
	if strings.Contains(productId, CONDUIT_PRODUCT_ID_PREFIX) {
		log.Printf("[INFO] setSerialPortName - Multitech Conduit detected: %s\n", productId)
		//We are running on a Multitech Conduit
		// 2. Determine which port (ap1 or ap2) has a product ID of MTAC-GPIOB
		portProductID := getProductId("ap1")
		if strings.Contains(portProductID, MTACGPIO_PRODUCT_ID) {
			log.Printf("[INFO] setSerialPortName - MTAC-GPIO detected on ap1\n")
			serialPortName = "ap1"
		} else {
			portProductID := getProductId("ap2")
			if strings.Contains(portProductID, MTACGPIO_PRODUCT_ID) {
				log.Printf("[INFO] setSerialPortName - MTAC-GPIO detected on ap2\n")
				serialPortName = "ap2"
			} else {
				log.Printf("[ERROR] setSerialPortName - MTAC-GPIO not detected on ap1 or ap2\n")
			}
		}
	} else {
		log.Printf("[ERROR] setSerialPortName - Not running on a multitech conduit\n")
		serialPortName = ""
	}
}

func getProductId(portName string) string {
	port := ""
	if portName != "" {
		port += portName + "/"
	}
	port += "product-id"

	cmd := exec.Command(MTSIO_CMD, "show", port)
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		log.Printf("[ERROR] getProductId - ERROR executing mts-io-sysfs command: %s\n", err.Error())
		return ""
	} else {
		log.Printf("[DEBUG] getProductId - Command response received: %s\n", out.String())
		return out.String()
	}
}

func watchFiles() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("[ERROR] watchFiles - error creating file watcher: %s\n", err.Error())
		log.Fatal(err)
	}
	defer watcher.Close()

	done := make(chan bool)
	go func(closeChannel chan bool) {
		for {
			select {
			case event := <-watcher.Events:
				log.Printf("[DEBUG] watchFiles - event detected: %#v\n", event)
				if event.Op&fsnotify.Write == fsnotify.Write {
					log.Printf("[DEBUG] watchFiles - modified file: %s\n", event.Name)
					//Need to get the last part of the file name
					_, file := filepath.Split(event.Name)
					// if this change is from a digital output that is currently cycling, ignore the change
					if _, ok := activeCycles[file]; ok {
						log.Println("[DEBUG] watchFiles - change was caused from an active cycle, not publishing event")
						break
					}
					if ndx, err := strconv.Atoi(event.Name[len(event.Name)-1:]); err == nil {
						if val, err := executeMtsioCommand(MTSIO_READ, file, nil); err == nil {
							if strings.Contains(file, DIGITAL_INPUT_PREFIX) {
								currentValues["digital"].(map[string]interface{})["input"].(map[string]interface{})["Data"].([]float64)[ndx] = val.(float64)
								storeGpioValueInCollection(DIGITAL_INPUT_PREFIX+strconv.Itoa(ndx), val.(float64))
							} else if strings.Contains(event.Name, DIGITAL_OUTPUT_PREFIX) {
								currentValues["digital"].(map[string]interface{})["output"].(map[string]interface{})["Data"].([]float64)[ndx] = val.(float64)
								storeGpioValueInCollection(DIGITAL_OUTPUT_PREFIX+strconv.Itoa(ndx), val.(float64))
							} else if strings.Contains(event.Name, ANALOG_INPUT_PREFIX) {
								currentValues["analog"].(map[string]interface{})["input"].(map[string]interface{})["Data"].([]float64)[ndx] = val.(float64)
								storeGpioValueInCollection(ANALOG_INPUT_PREFIX+strconv.Itoa(ndx), val.(float64))
							}
						}
					} else {
						log.Printf("[ERROR] watchFiles - modified file: %s\n", err)
					}

					jsonResp := make(map[string]interface{})
					jsonResp["success"] = true
					jsonResp["action"] = gpioRead
					jsonResp["gpio"] = currentValues

					log.Println("[DEBUG] watchFiles - Publishing GPIO")
					publishGpioResponse(jsonResp)
				}
			case err := <-watcher.Errors:
				log.Printf("watchFiles - error: %s\n", err.Error())
			case _ = <-endFileWatcherChannel:
				//End the current go routine when the stop signal is received
				log.Println("[INFO] watchFiles - Stopping file watcher")
				done <- true
				return
			}
		}
	}(done)

	//We need to watch the individual files rather than the directory
	log.Println("[DEBUG] watchFiles - adding files to watcher")
	for i := 0; i < 4; i++ {
		log.Printf("[DEBUG] watchFiles - adding digital input file to watcher: %s\n",
			GPIO_DIRECTORY+"/"+serialPortName+"/"+fmt.Sprintf("%s%d", DIGITAL_INPUT_PREFIX, i))
		if err := watcher.Add(GPIO_DIRECTORY + "/" +
			serialPortName + "/" +
			fmt.Sprintf("%s%d", DIGITAL_INPUT_PREFIX, i)); err != nil {
			log.Printf("[ERROR] watchFiles - error adding file to watcher: %s\n", err.Error())
			log.Fatal(err)
		}
		log.Printf("[DEBUG] watchFiles - adding digital output file to watcher: %s\n",
			GPIO_DIRECTORY+"/"+serialPortName+"/"+fmt.Sprintf("%s%d", DIGITAL_OUTPUT_PREFIX, i))
		if err := watcher.Add(GPIO_DIRECTORY + "/" +
			serialPortName + "/" +
			fmt.Sprintf("%s%d", DIGITAL_OUTPUT_PREFIX, i)); err != nil {
			log.Printf("[ERROR] watchFiles - error adding file to watcher: %s\n", err.Error())
			log.Fatal(err)
		}
		if i < 3 {
			log.Printf("[DEBUG] watchFiles - adding analog input file to watcher: %s\n",
				GPIO_DIRECTORY+"/"+serialPortName+"/"+fmt.Sprintf("%s%d", ANALOG_INPUT_PREFIX, i))
			if err := watcher.Add(GPIO_DIRECTORY + "/" +
				serialPortName + "/" +
				fmt.Sprintf("%s%d", ANALOG_INPUT_PREFIX, i)); err != nil {
				log.Printf("[ERROR] watchFiles - error adding file to watcher: %s\n", err.Error())
				log.Fatal(err)
			}
		}
	}

	<-done
}

func publishGpioResponse(respJson map[string]interface{}) {
	//Create the response topic
	theTopic := topicRoot + "/"

	log.Printf("[DEBUG] publishGpioResponse - respJson['action'] = %s\n", respJson["action"])

	if respJson["action"].(string) == gpioRead {
		theTopic += gpioRead
	} else {
		theTopic += gpioWrite
	}
	theTopic += "/response"

	//Add a timestamp to the payload
	respJson["timestamp"] = time.Now().Format(JavascriptISOString)

	respStr, err := json.Marshal(respJson)
	if err != nil {
		log.Printf("[ERROR] executeCommand - ERROR marshalling json response: %s\n", err.Error())
	} else {
		log.Printf("[DEBUG] executeCommand - Publishing response %s to topic %s\n", string(respStr), theTopic)

		//Publish the response
		err = publish(theTopic, string(respStr))
		if err != nil {
			log.Printf("[ERROR] subscribeWorker - ERROR publishing to topic: %s\n", err.Error())
		}
	}
}

func initGpioCollection() {
	// to make life easier let's clear out all old rows, each di/do/ai will have it's own row. we are assuming the collection has the needed columns
	if gpioDataCollID != "" {
		log.Println("[INFO] initGpioCollection - clearing old data from gpio collection")
		if err := cbBroker.client.DeleteData(gpioDataCollID, nil); err != nil {
			log.Printf("[ERROR] initGpioCollection - failed to delete old data from gpio data collection: %s\n", err.Error())
		}
		initialData := make([]map[string]interface{}, 0)
		for i := 0; i < 4; i++ {
			initialData = append(initialData, map[string]interface{}{"gpio_id": DIGITAL_INPUT_PREFIX + strconv.Itoa(i), "input_value": -1})
			initialData = append(initialData, map[string]interface{}{"gpio_id": DIGITAL_OUTPUT_PREFIX + strconv.Itoa(i), "input_value": -1})
			//only have 3 analog inputs on the gpio board
			if i < 3 {
				initialData = append(initialData, map[string]interface{}{"gpio_id": ANALOG_INPUT_PREFIX + strconv.Itoa(i), "input_value": -1})
			}
		}
		if err := cbBroker.client.InsertData(gpioDataCollID, initialData); err != nil {
			log.Printf("[ERROR] initGpioCollection - failed to create gpio data: %s\n", err.Error())
		}
	}
}

func storeGpioValueInCollection(inputName string, inputValue float64) {
	if gpioDataCollID != "" {
		log.Println("[INFO] storeGpioValueInCollection - gpio collection ID provided, updating value")
		query := cb.NewQuery()
		query.EqualTo("gpio_id", inputName)
		changes := map[string]interface{}{
			"input_value": inputValue,
		}
		if err := cbBroker.client.UpdateData(gpioDataCollID, query, changes); err != nil {
			log.Printf("[ERROR] storeGpioValueInCollection - failed to update gpio data: %s\n", err.Error())
		}
	}
}
