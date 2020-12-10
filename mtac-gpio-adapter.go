package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	cb "github.com/clearblade/Go-SDK"
	mqttTypes "github.com/clearblade/mqtt_parsing"
	mqtt "github.com/clearblade/paho.mqtt.golang"
	"github.com/hashicorp/logutils"
)

const (
	msgSubscribeQOS        = 0
	msgPublishQOS          = 0
	conduitProductIDPrefix = "MTCDT"
	mtacGPIOProductID      = "MTAC-GPIOB"
	defaultTopicRoot       = "mtac-gpio"
	requestMQTTTopic       = "<topic_root>/request/<gpio_id>"
	changeMQTTTopic        = "<topic_root>/change/<gpio_id>"
	gpioDirectory          = "/sys/devices/platform/mts-io/<serial_port>/<gpio_id>"
	javascriptISOString    = "2006-01-02T15:04:05.000Z07:00"
	gpioOn                 = 0
	gpioOff                = 1
)

var (
	sysKey              string
	sysSec              string
	deviceName          string
	activeKey           string
	platformURL         string
	messagingURL        string
	logLevel            string
	adapterConfigCollID string
	cbClient            *cb.DeviceClient
	config              adapterConfig
	serialPort          = ""
	currentValues       gpioValues
	readOnlyGPIOIDs     = []string{"adc0", "adc1", "adc2", "din0", "din1", "din2", "din3"}
	readWriteGPIOIDs    = []string{"dout0", "dout1", "dout2", "dout3", "led1", "led2"}

	topicRoot = "mtac-gpio"
)

type gpio struct {
	GpioID           string
	FilePath         string
	Value            int
	File             *os.File
	ActiveCycle      bool
	OnInterval       int
	OffInterval      int
	CycleQuitChannel chan int
}

type gpioValues struct {
	Mutex  *sync.Mutex
	Values map[string]*gpio
}

// no current adapter settings
type adapterSettings struct {
	WatchInputs     []string `json:"watchInputs"`
	PollingInterval int      `json:"pollingInterval"`
}

type adapterConfig struct {
	AdapterSettings adapterSettings `json:"adapter_settings"`
	TopicRoot       string          `json:"topic_root"`
}

type requestStruct struct {
	Type        string `json:"type"`
	Value       int    `json:"value"`
	OnInterval  int    `json:"on_interval"`
	OffInterval int    `json:"off_interval"`
}

type changeStruct struct {
	OldValue  int    `json:"old_value"`
	NewValue  int    `json:"new_value"`
	Timestamp string `json:"timestamp"`
}

func init() {
	flag.StringVar(&sysKey, "systemKey", "", "system key (required)")
	flag.StringVar(&sysSec, "systemSecret", "", "system secret (required)")
	flag.StringVar(&deviceName, "deviceName", "mtacGpioAdapter", "name of device (optional)")
	flag.StringVar(&activeKey, "activeKey", "", "active key for device authentication (required)")
	flag.StringVar(&platformURL, "platformURL", "http://localhost:9000", "platform url (optional)")
	flag.StringVar(&messagingURL, "messagingURL", "localhost:1883", "messaging URL (optional)")
	flag.StringVar(&logLevel, "logLevel", "info", "The level of logging to use. Available levels are 'debug, 'info', 'warn', 'error', 'fatal' (optional)")
	flag.StringVar(&adapterConfigCollID, "adapterConfigCollectionID", "", "The ID of the data collection used to house adapter configuration (required)")

}

func usage() {
	log.Printf("Usage: mtacGpioAdapter [options]\n\n")
	flag.PrintDefaults()
}

func validateFlags() {
	flag.Parse()

	if sysKey == "" || sysSec == "" || activeKey == "" || adapterConfigCollID == "" {
		log.Println("ERROR - Missing required flags")
		flag.Usage()
		os.Exit(1)
	}
}

func main() {
	log.Println("Starting mtacGpioAdapter...")

	flag.Usage = usage
	validateFlags()

	rand.Seed(time.Now().UnixNano())

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	filter := &logutils.LevelFilter{
		Levels:   []logutils.LogLevel{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"},
		MinLevel: logutils.LogLevel(strings.ToUpper(logLevel)),
		Writer:   os.Stdout,
	}
	log.SetOutput(filter)

	setSerialPortName()
	initGPIOValues()
	initClearBlade()
	connectClearBlade()

	log.Println("[DEBUG] main - starting info log ticker")

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			log.Printf("[INFO] Listening for GPIO changes or write requests...")
		}
	}
}

func initClearBlade() {
	log.Println("[DEBUG] initClearBlade - initializing clearblade")
	cbClient = cb.NewDeviceClientWithAddrs(platformURL, messagingURL, sysKey, sysSec, deviceName, activeKey)
	log.Println("[INFO] initClearBlade - Trying again every 2 seconds...")
	for err := authClearBladeClient(cbClient); err != nil; {
		log.Printf("[ERROR] initClearBlade - Error authenticating %s: %s\n", deviceName, err.Error())
		time.Sleep(time.Second * 2)
		err = authClearBladeClient(cbClient)
	}
	//log.Println("[INFO] initClearBlade - clearblade successfully initialized")

	//setAdapterConfig(cbClient)

}

func authClearBladeClient(client cb.Client) error {
	if _, err := cbClient.Authenticate(); err != nil {
		log.Printf("[ERROR] initOtherCbClient - Error authenticating ClearBlade: %s\n", err.Error())
		return err
	}
	//10Dec2020: post to topic_root/feedback
	var feedbackMsg = "initClearBlade - clearblade successfully initialized"
	topic := fmt.Sprintf("%s/feedback", config.TopicRoot)
	log.Println(fmt.Sprintf("[INFO] %s", feedbackMsg))
	cbClient.Publish(topic, []byte(feedbackMsg), msgPublishQOS)
	setAdapterConfig(cbClient)
	return nil
}

func setAdapterConfig(client cb.Client) {
	log.Println("[INFO] setAdapterConfig - Fetching adapter config")

	query := cb.NewQuery()
	query.EqualTo("adapter_name", deviceName)

	log.Println("[DEBUG] setAdapterConfig - Executing query against table " + adapterConfigCollID)
	results, err := client.GetData(adapterConfigCollID, query)
	if err != nil {
		log.Fatalf("[FATAL] setAdapterConfig - Error fetching adapter config: %s", err.Error())
	}

	data := results["DATA"].([]interface{})

	if len(data) == 0 {
		log.Fatalf("[FATAL] - setAdapterConfig - No configuration found for adapter with name: %s", deviceName)
	}

	config = adapterConfig{TopicRoot: topicRoot}

	configData := data[0].(map[string]interface{})
	log.Printf("[DEBUG] setAdapterConfig - fetched config:\n%+v\n", data)
	if configData["topic_root"] != nil {
		config.TopicRoot = configData["topic_root"].(string)
	}
	if configData["adapter_settings"] == nil {
		log.Fatalln("[FATAL] setAdapterConfig - No adapter settings required, this is required")
	}
	var settings adapterSettings
	if err := json.Unmarshal([]byte(configData["adapter_settings"].(string)), &settings); err != nil {
		log.Fatalf("[FATAL] setAdapterConfig - Failed to parse adapter_settings: %s", err.Error())
	}

	config.AdapterSettings = settings

	// if config.AdapterSettings.WatchInputs.IS{
	// 	log.Fatalln("[FATAL] setAdapterConfig - No messaging URL defined for broker config adapter_settings")
	// }

	log.Printf("[DEBUG] setAdapterConfig - Using adapter settings:\n%+v\n", config)
}

func setSerialPortName() {
	log.Println("[DEBUG] setSerialPortName - determining serial port")
	productID := gpio{
		GpioID:   "product-id",
		FilePath: "/sys/devices/platform/mts-io/product-id",
	}
	productID.openReadOnlyFile()
	productIDString, err := productID.getRawStringValueFromFile()
	if err != nil {
		log.Fatalf("[FATAL] setSerialPortName - failed to read product id: %s", err.Error())
	}
	if strings.HasPrefix(productIDString, conduitProductIDPrefix) {
		for x := 1; x < 3; x++ {
			apString := "ap" + strconv.Itoa(x)
			log.Printf("[DEBUG] setSerialPortName - checking %s for gpio card", apString)
			apProductID := gpio{
				GpioID:   "product-id",
				FilePath: "/sys/devices/platform/mts-io/" + apString + "/product-id",
			}
			apProductID.openReadOnlyFile()
			apProductIDString, err := apProductID.getRawStringValueFromFile()
			if err != nil {
				log.Fatalf("[FATAL] setSerialPortName - failed to read %s product-id: %s", apString, err.Error())
			}
			if apProductIDString == mtacGPIOProductID {
				serialPort = apString
				break
			}
		}
	} else {
		log.Fatalf("[FATAL] setSerialPortName - not running on a multitech conduit gateway")
	}
	if serialPort == "" {
		log.Fatalln("[FATAL] setSerialPortName - no gpio board found")
	}
	log.Println("[INFO] setSerialPointName - serial port successfully determined")
}

func initGPIOValues() {
	log.Println("[DEBUG] initGPIOValues - opening GPIO files and loading initial values")
	currentValues = gpioValues{
		Mutex:  &sync.Mutex{},
		Values: make(map[string]*gpio),
	}
	for _, gpioID := range readOnlyGPIOIDs {
		thisGPIO := &gpio{
			GpioID: gpioID,
		}
		thisGPIO.setFilePath()
		thisGPIO.openReadOnlyFile()
		thisGPIO.readValueFromFile()
		currentValues.Mutex.Lock()
		currentValues.Values[gpioID] = thisGPIO
		currentValues.Mutex.Unlock()
		log.Printf("[DEBUG] initGPIOValues - initial value for %s is : %d\n", thisGPIO.GpioID, thisGPIO.Value)
	}
	for _, gpioID := range readWriteGPIOIDs {
		thisGPIO := &gpio{
			GpioID: gpioID,
		}
		thisGPIO.setFilePath()
		thisGPIO.openReadWriteFile()
		thisGPIO.readValueFromFile()
		currentValues.Mutex.Lock()
		currentValues.Values[gpioID] = thisGPIO
		currentValues.Mutex.Unlock()
		log.Printf("[DEBUG] initGPIOValues - initial value for %s is : %d\n", thisGPIO.GpioID, thisGPIO.Value)
	}
	log.Println("[INFO] initGPIOValues - initial gpio values loaded successfully")
}

func initFilePolling() {
	for x := 0; x < len(config.AdapterSettings.WatchInputs); x++ {
		go startPolling(config.AdapterSettings.WatchInputs[x])
	}
}

func startPolling(gpioID string) {
	timestamp := time.Now().Format(javascriptISOString)
	ticker := time.NewTicker(time.Duration(config.AdapterSettings.PollingInterval) * time.Millisecond)
	for t := range ticker.C {
		currentValues.Mutex.Lock()
		changedGPIO := currentValues.Values[gpioID]
		currentValues.Mutex.Unlock()
		oldValue := changedGPIO.Value
		if err := changedGPIO.readValueFromFile(); err != nil {
			//10Dec2020: post to topic_root/error
			var errorMsg = fmt.Sprintf("initFilePolling - failed to read new value: %s", err.Error())
			log.Printf("[ERROR] %s", errorMsg)
			topic := fmt.Sprintf("%s/error", config.TopicRoot)
			cbClient.Publish(topic, []byte(errorMsg), msgPublishQOS)
			break
		}
		newValue := changedGPIO.Value
		if oldValue != newValue {
			log.Println("[INFO] initFilePolling - got a new value tick at ", t)
			log.Printf("[INFO] new value for %s is %d\n", gpioID, newValue)
			publishGPIOChange(gpioID, oldValue, newValue, timestamp)
		} else {
			//log.Println("[DEBUG] initFilePolling - value didn't actually change, not doing anything at tick ", t)
		}
	}
	defer ticker.Stop()
}

// func initFileWatchers() {
// 	log.Println("[DEBUG] initFileWatchers - creating file watchers for digital outputs")
// 	watcher, err := fsnotify.NewWatcher()
// 	if err != nil {
// 		log.Fatalf("[FATAL] initFileWatchers - failed to create file watcher: %s", err.Error())
// 	}
// 	defer watcher.Close()
// 	for _, gpioID := range readOnlyGPIOIDs {
// 		currentValues.Mutex.Lock()
// 		path := currentValues.Values[gpioID].FilePath
// 		currentValues.Mutex.Unlock()
// 		log.Printf("[DEBUG] initFileWatchers - adding watcher for file: %s", path)
// 		if err := watcher.Add(path); err != nil {
// 			log.Fatalf("[FATAL] initFileWatchers - failed to add file watcher: %s", err.Error())
// 		}
// 	}
// 	log.Println("[INFO] initFileWatchers - file watchers successfully created for digital outputs")

// 	for {
// 		select {
// 		case event := <-watcher.Events: //blocks until watcher.events returns something
// 			timestamp := time.Now().Format(javascriptISOString)
// 			log.Printf("[DEBUG] initFileWatchers - event detected: %+v\n", event)

// 			//poll inputs here
// 			if event.Op&fsnotify.Write == fsnotify.Write {
// 				log.Printf("[DEBUG] initFileWatchers - modified file: %s\n", event.Name)
// 				_, gpioID := filepath.Split(event.Name)
// 				currentValues.Mutex.Lock()
// 				changedGPIO := currentValues.Values[gpioID]
// 				currentValues.Mutex.Unlock()
// 				// if this change is from a digital output that is currently cycling, ignore it
// 				if changedGPIO.ActiveCycle {
// 					log.Println("[DEBUG] initFileWatchers - change was caused from an active cycle, ignoring it")
// 					break
// 				}
// 				oldValue := changedGPIO.Value
// 				if err := changedGPIO.readValueFromFile(); err != nil {
// 					log.Printf("[ERROR] initFileWatchers - failed to read new value: %s", err.Error())
// 					break
// 				}
// 				newValue := changedGPIO.Value
// 				if oldValue != newValue {
// 					publishGPIOChange(gpioID, oldValue, newValue, timestamp)
// 				} else {
// 					log.Println("[DEBUG] initFileWatchers - value didn't actually change, not doing anything")
// 				}
// 			}
// 		case err := <-watcher.Errors:
// 			log.Printf("[ERROR] initFileWatchers - unexpected error: %s", err.Error())
// 		}
// 	}
// }

func connectClearBlade() {
	log.Println("[INFO] connectClearBlade - connecting ClearBlade MQTT")
	callbacks := cb.Callbacks{OnConnectCallback: onConnect, OnConnectionLostCallback: onConnectLost}
	if err := cbClient.InitializeMQTTWithCallback(deviceName+"-"+strconv.Itoa(rand.Intn(10000)), "", 30, nil, nil, &callbacks); err != nil {
		log.Fatalf("[FATAL] connectClearBlade - Unable to connect ClearBlade MQTT: %s", err.Error())
	}
}

func onConnect(client mqtt.Client) {
	//10Dec2020: post to topic_root/feedback
	var feedbackMsg = "onConnect - ClearBlade MQTT successfully connected"
	log.Println(fmt.Sprintf("[INFO] %s", feedbackMsg))
	feedbackTopic := fmt.Sprintf("%s/feedback", config.TopicRoot)
	cbClient.Publish(feedbackTopic, []byte(feedbackMsg), msgPublishQOS)

	var cbSubChannel <-chan *mqttTypes.Publish
	var err error
	topic := strings.Replace(requestMQTTTopic, "<topic_root>", config.TopicRoot, 1)
	topic = strings.Replace(topic, "<gpio_id>", "+", 1)
	for cbSubChannel, err = cbClient.Subscribe(topic, msgSubscribeQOS); err != nil; {
		//10Dec2020: post to topic_root/error
		var errorMsg = fmt.Sprintf("onConnect - Failed to subscribe to MQTT topic: %s\n", err.Error())
		log.Printf("[ERROR] %s", errorMsg)
		errorTopic := fmt.Sprintf("%s/error", config.TopicRoot)
		cbClient.Publish(errorTopic, []byte(errorMsg), msgPublishQOS)

		log.Println("[INFO] onConnect - retrying subscribe in 30 seconds...")
		cbSubChannel, err = cbClient.Subscribe(topic, msgSubscribeQOS)
	}
	//go initFileWatchers()
	go initFilePolling()
	go subscribeWorker(cbSubChannel)
}

func onConnectLost(client mqtt.Client, connerr error) {
	//10Dec2020: post to topic_root/error
	var errorMsg = fmt.Sprintf("onConnectLost - ClearBlade MQTT lost connection: %s", connerr.Error())
	log.Printf("[ERROR] %s", errorMsg)
	errorTopic := fmt.Sprintf("%s/error", config.TopicRoot)
	cbClient.Publish(errorTopic, []byte(errorMsg), msgPublishQOS)

	// reconnect logic should be handled by go/paho sdk under the covers
}

func subscribeWorker(onPubChannel <-chan *mqttTypes.Publish) {
	for {
		select {
		case message, ok := <-onPubChannel:
			if ok {
				log.Printf("[DEBUG] subscribeWorker - received raw message %s on topic %s\n", string(message.Payload), message.Topic.Whole)
				if len(message.Topic.Split) == 3 && message.Topic.Split[1] == "request" {
					var requestMsg requestStruct
					if err := json.Unmarshal(message.Payload, &requestMsg); err != nil {
						log.Printf("[ERROR] subscribeWorker - failed to unmarshal incoming mqtt request message: %s\n", err.Error())
						break
					}
					gpioID := message.Topic.Split[len(message.Topic.Split)-1]
					currentValues.Mutex.Lock()
					var theGPIO *gpio
					if theGPIO, ok = currentValues.Values[gpioID]; !ok {
						currentValues.Mutex.Unlock()
						log.Printf("[ERROR] subscribeWorker - unexpected gpio id: %s\n", gpioID)
						break
					}
					currentValues.Mutex.Unlock()
					switch requestMsg.Type {
					case "write":
						log.Printf("[DEBUG] subscribeWorker - processing gpio write request: %+v\n", requestMsg)
						if err := theGPIO.writeNewValueToFile(requestMsg.Value); err != nil {
							//10Dec2020: post to topic_root/error
							var errorMsg = fmt.Sprintf("subscribeWorker - failed to write gpio change: %s", err.Error())
							log.Printf("[ERROR] %s", errorMsg)
							errorTopic := fmt.Sprintf("%s/error", config.TopicRoot)
							cbClient.Publish(errorTopic, []byte(errorMsg), msgPublishQOS)
						}
						break
					case "start_cycle":
						log.Printf("[DEBUG] subscribeWorker - processing gpio start_cycle request: %+v\n", requestMsg)
						if theGPIO.ActiveCycle {
							log.Printf("[ERROR]")
							break
						}
						theGPIO.startGPIOCycle(requestMsg.OnInterval, requestMsg.OffInterval)
						break
					case "end_cycle":
						log.Printf("[DEBUG] subscribeWorker - processing gpio end_cycle request: %+v\n", requestMsg)
						theGPIO.stopGPIOCycle(requestMsg.Value)
						break
					default:
						log.Printf("[ERROR] subscribeWorker - unexpected request type: %s\n", requestMsg.Type)
						break
					}
				} else {
					log.Printf("[ERROR] subscribeWorker - Unepxected topic for request message: %s\n", message.Topic.Whole)
					break
				}
			}
		}
	}
}

func publishGPIOChange(gpioID string, oldValue, newValue int, timestamp string) {
	log.Println("[DEBUG] publishGPIO change - received gpio change to publish")
	//"<topic_root>/change/<gpio_id>"
	topic := strings.Replace(changeMQTTTopic, "<topic_root>", config.TopicRoot, 1)
	topic = strings.Replace(topic, "<gpio_id>", gpioID, 1)
	changeMessage := &changeStruct{
		OldValue:  oldValue,
		NewValue:  newValue,
		Timestamp: timestamp,
	}
	msgBytes, err := json.Marshal(changeMessage)
	if err != nil {
		log.Printf("[ERROR] publishGPIOChange - failed to marshal change message: %s\n", err.Error())
	}
	if err := cbClient.Publish(topic, msgBytes, msgPublishQOS); err != nil {
		log.Printf("[ERROR] publishGPIOChange - failed to publish change :%s\n", err.Error())
		return
	}
	log.Println("[DEBUG] publishGPIOChange - Change successfully published")
}

func (g *gpio) setFilePath() {
	log.Printf("[DEBUG] setFilePath - Setting file path for gpio id: %s\n", g.GpioID)
	g.FilePath = strings.Replace(gpioDirectory, "<serial_port>", serialPort, 1)
	g.FilePath = strings.Replace(g.FilePath, "<gpio_id>", g.GpioID, 1)
	log.Printf("[INFO] setFilePath - File path successully set for gpio id: %s\n", g.GpioID)
}

func (g *gpio) openReadOnlyFile() {
	log.Printf("[DEBUG] openReadOnlyFile - opening read only file: %s\n", g.FilePath)
	var err error
	g.File, err = os.Open(g.FilePath)
	if err != nil {
		log.Fatalf("[FATAL] openReadOnlyFile - Failed to open gpio file %s: %s", g.GpioID, err.Error())
	}
	log.Printf("[INFO] openReadOnlyFile - File successfully opened: %s\n", g.FilePath)
}

func (g *gpio) openReadWriteFile() {
	log.Printf("[DEBUG] openReadWriteFile - opening read write file: %s\n", g.FilePath)
	var err error
	g.File, err = os.OpenFile(g.FilePath, os.O_RDWR, 0644)
	if err != nil {
		log.Fatalf("[FATAL] openReadWriteFile - Failed to open gpio file %s: %s", g.GpioID, err.Error())
	}
	log.Printf("[INFO] openReadWriteFile - File successfully opened: %s\n", g.FilePath)
}

func (g *gpio) writeNewValueToFile(newValue int) error {
	log.Printf("[DEBUG] writeNewValueToFile - writing value %d for gpio id: %s\n", newValue, g.GpioID)
	if g.Value == newValue {
		log.Println("[DEBUG] writeNewValueToFile - provided new value is same as current, doing nothing")
		return nil
	}
	_, err := g.File.WriteAt([]byte(strconv.Itoa(newValue)), 0)
	if err != nil {
		log.Printf("[ERROR] failed to write value to file for gpio %s: %s\n", g.GpioID, err.Error())
		return err
	}
	g.Value = newValue
	log.Printf("[INFO] writeNewValueToFile - successfully wrote new value for gpio id: %s\n", g.GpioID)
	return nil
}

func (g *gpio) readValueFromFile() error {
	//log.Printf("[DEBUG] readValueFromFile - reading value for gpio id: %s\n", g.GpioID)
	rawString, err := g.getRawStringValueFromFile()
	if err != nil {
		return err
	}
	g.Value, err = strconv.Atoi(rawString)
	if err != nil {
		log.Printf("[ERROR] Failed to convert value to int for gpio %s: %s\n", g.GpioID, err.Error())
		return err
	}
	//log.Printf("[INFO] readValueFromFile - read value %d from gpio id: %s\n", g.Value, g.GpioID)
	return nil
}

func (g *gpio) getRawStringValueFromFile() (string, error) {
	//log.Printf("[DEBUG] getRawStringValueFromFile - reading value for gpio id: %s\n", g.GpioID)
	g.File.Seek(0, 0)
	b, err := ioutil.ReadAll(g.File)
	if err != nil {
		log.Printf("[ERROR] getRawStringValueFromFile - Failed to read value from file for gpio %s: %s\n", g.GpioID, err.Error())
		return "", err
	}
	stringValue := strings.TrimRight(string(b), "\n")
	//log.Printf("[INFO] getRawStringValueFromFile - read value %s from gpio id: %s\n", stringValue, g.GpioID)
	return stringValue, nil
}

func (g *gpio) startGPIOCycle(onInterval, offInterval int) {
	log.Printf("[DEBUG] startGPIOCycle - starting gpio cycle for gpio id: %s\n", g.GpioID)
	g.OnInterval = onInterval
	g.OffInterval = offInterval
	g.CycleQuitChannel = make(chan int)
	g.ActiveCycle = true
	go g.cycleGPIO()
	log.Printf("[INFO] startGPIOCycle - successully started gpio cycle for gpio id: %s\n", g.GpioID)
}

func (g *gpio) cycleGPIO() {
	totalInterval := g.OnInterval + g.OffInterval
	if err := g.writeNewValueToFile(gpioOn); err != nil {
		log.Printf("[ERROR] cycleGPIO - failed to turn on GPIO: %s\n", err.Error())
	}
	onTicker := time.NewTicker(time.Duration(totalInterval) * time.Second)
	defer onTicker.Stop()
	time.Sleep(time.Duration(g.OnInterval) * time.Second)
	offTicker := time.NewTicker(time.Duration(totalInterval) * time.Second)
	defer offTicker.Stop()
	for {
		select {
		case newValue := <-g.CycleQuitChannel:
			if err := g.writeNewValueToFile(newValue); err != nil {
				log.Printf("[ERROR] cycleGPIO - failed to set new value when ending cycle: %s\n", err.Error())
			}
			g.ActiveCycle = false
			g.CycleQuitChannel = nil
			return
		case <-onTicker.C:
			if err := g.writeNewValueToFile(gpioOff); err != nil {
				log.Printf("[ERROR] cycleGPIO - failed to turn off GPIO: %s\n", err.Error())
			}
			break
		case <-offTicker.C:
			if err := g.writeNewValueToFile(gpioOn); err != nil {
				log.Printf("[ERROR] cycleGPIO - failed to turn off GPIO: %s\n", err.Error())
			}
			break
		}
	}
}

func (g *gpio) stopGPIOCycle(newValue int) {
	log.Printf("[DEBUG] stopGPIOCycle - stopping gpio cycle for gpio id: %s\n", g.GpioID)
	if g.ActiveCycle {
		g.CycleQuitChannel <- newValue
	} else {
		log.Println("[ERROR] stopGPIOCycle - gpio is not currently cycling")
	}
	log.Printf("[INFO] stopGPIOCycle - successully stopped gpio cycle for gpio id: %s\n", g.GpioID)
}
