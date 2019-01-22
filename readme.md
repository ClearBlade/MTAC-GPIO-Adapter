# mtacGpioAdapter Adapter

The __mtacGpioAdapter__ adapter provides the ability for the ClearBlade platform to interface with the MultiTech __MTAC-GPIO__ accessory card. (https://www.multitech.com/models/94557076LF). Internally, the __mtacGpioAdapter__ adapter utilizes the mts-io package provided by Multitech. The mts_io package provides a sysfs interface to the Multitech Conduit’s various I/O abilities and for the ID info stored in the on-board EEPROM. Sysfs attributes are exported at /sys/devices/platform/mts-io on the device. A shell script mts-io-sysfs is provided as a simple wrapper to the sysfs attributes.


The adapter subscribes to MQTT topics which are used to interact with the mts-io-sysfs utility. The adapter publishes any data retrieved from the mts-io-sysfs utility to MQTT topics so that the ClearBlade Platform is able to retrieve and process mts-io-sysfs utility related data..

## mts-io-sysfs Command Documentation

```
Usage: mts-io-sysfs [ OPTIONS ] OBJECT [--] [ ARGUMENTS ]
where  OBJECT := {
         show SHOW-NAME |
         store STORE-NAME |
       }

       SHOW-NAME := {
         ap1/product-id
         ap1/device-id
         ap1/rts-override
         ap1/vendor-id
         ap1/reset
         ap1/rs4xx-term-res
         ap1/hw-version
         ap1/serial-mode
         ap2/product-id
         ap2/adc0
         ap2/adc1
         ap2/adc2
         ap2/din0
         ap2/din1
         ap2/din2
         ap2/din3
         ap2/led1
         ap2/led2
         ap2/device-id
         ap2/vendor-id
         ap2/dout0
         ap2/dout1
         ap2/dout2
         ap2/dout3
         ap2/reset
         ap2/dout-enable
         ap2/hw-version
         capability/adc
         capability/din
         capability/gps
         capability/dout
         capability/lora
         capability/wifi
         capability/bluetooth
         device-id
         eth-reset
         gnss-int
         gnss-reset
         gpiob/product-id
         gpiob/adc0
         gpiob/adc1
         gpiob/adc2
         gpiob/din0
         gpiob/din1
         gpiob/din2
         gpiob/din3
         gpiob/led1
         gpiob/led2
         gpiob/device-id
         gpiob/vendor-id
         gpiob/dout0
         gpiob/dout1
         gpiob/dout2
         gpiob/dout3
         gpiob/reset
         gpiob/dout-enable
         gpiob/hw-version
         has-radio
         hw-version
         imei
         led-a
         led-b
         led-c
         led-cd
         led-d
         led-sig1
         led-sig2
         led-sig3
         led-status
         mac-eth
         mfser/product-id
         mfser/device-id
         mfser/rts-override
         mfser/vendor-id
         mfser/reset
         mfser/rs4xx-term-res
         mfser/hw-version
         mfser/serial-mode
         product-id
         radio-power
         radio-reset
         radio-reset-backoff-index
         radio-reset-backoff-seconds
         radio-reset-backoffs
         reset
         reset-monitor
         reset-monitor-intervals
         usbhub-reset
         uuid
         vendor-id
         wifi-bt-int
         wifi-bt-lpmode
         wifi-bt-lpwkup
         wifi-bt-reset
         wifi-bt-ulpwkup
       }

       STORE-NAME := {
         ap1/rts-override BOOLEAN
         ap1/reset BOOLEAN
         ap1/rs4xx-term-res BOOLEAN
         ap1/serial-mode { loopback | rs232 | rs485-half | rs422-485-full }
         ap2/led1 BOOLEAN
         ap2/led2 BOOLEAN
         ap2/dout0 BOOLEAN
         ap2/dout1 BOOLEAN
         ap2/dout2 BOOLEAN
         ap2/dout3 BOOLEAN
         ap2/reset BOOLEAN
         ap2/dout-enable BOOLEAN
         eth-reset BOOLEAN
         gnss-int BOOLEAN
         gnss-reset BOOLEAN
         gpiob/led1 BOOLEAN
         gpiob/led2 BOOLEAN
         gpiob/dout0 BOOLEAN
         gpiob/dout1 BOOLEAN
         gpiob/dout2 BOOLEAN
         gpiob/dout3 BOOLEAN
         gpiob/reset BOOLEAN
         gpiob/dout-enable BOOLEAN
         has-radio BOOLEAN
         led-a BOOLEAN
         led-b BOOLEAN
         led-c BOOLEAN
         led-cd BOOLEAN
         led-d BOOLEAN
         led-sig1 BOOLEAN
         led-sig2 BOOLEAN
         led-sig3 BOOLEAN
         led-status BOOLEAN
         mfser/rts-override BOOLEAN
         mfser/reset BOOLEAN
         mfser/rs4xx-term-res BOOLEAN
         mfser/serial-mode { loopback | rs232 | rs485-half | rs422-485-full }
         radio-power BOOLEAN
         radio-reset { 0 }
         radio-reset-backoff-index BOOLEAN
         radio-reset-backoffs BOOLEAN
         reset-monitor { pid short-signal long-signal [extra-long-signal] }
         reset-monitor-intervals { short-interval long-interval }
         usbhub-reset BOOLEAN
         wifi-bt-int BOOLEAN
         wifi-bt-lpmode BOOLEAN
         wifi-bt-lpwkup BOOLEAN
         wifi-bt-reset BOOLEAN
         wifi-bt-ulpwkup BOOLEAN
       }

       OPTIONS := {
         --verbose
       }

       BOOLEAN := { OFF | ON }
       OFF := 0
       ON := 1
```

## MQTT Topic Structure
The mts-io adapter utilizes MQTT messaging to communicate with the ClearBlade Platform. The mts-io adapter will subscribe to a specific topic in order to handle mts-io-sysfs utility requests. Additionally, the mts-io adapter will publish messages to MQTT topics in order to communicate the results of requests to client applications. The topic structures utilized by the mts-io adapter are as follows:

  * Read mts-io data request: {__TOPIC ROOT__}/read/request
  * Read mts-io data response: {__TOPIC ROOT__}/read/response
  * Write mts-io data request: {__TOPIC ROOT__}/write/request
  * Write mts-io data response: {__TOPIC ROOT__}/write/response

### MQTT Payloads
The JSON payloads expected by and returned from the __mtacGpioAdapter__ adapter should have the following formats:

#### Read MTAC-GPIO data request

The json request should be structured as follows:	
{
  “action”: “read”,
  "gpio": {
    "digital": {
      "input": {
        “StartAddress”: 0,
        “AddressCount”: 4
      },
      "output": {
        “StartAddress”: 0,
        “AddressCount”: 4
      },
    },
    "analog": {
      "input": {
        “StartAddress”: 0,
        “AddressCount”: 4
      }
    },
  }
}

#### Write mts-io data request

The json request should resemble the following:	
{
  “action”: “write”,
  "gpio": {
    "digital": {
      “StartAddress”: 0,
      “AddressCount”: 4,
      “Data”: [true, false, false, true]
    }
  }
}

#### Read mts-io data response

	The json response will resemble the following:
	
{
  “success”: true|false,
  “error”: “the error message”,
  “timestamp”: the_timestamp,
  “action”: “read”,
  "gpio": {
    "digital": {
      "input": {
        “StartAddress”: 0,
        “AddressCount”: 4,
        “Data”: [true, false, false, true]
      },
      "output": {
        “StartAddress”: 0,
        “AddressCount”: 4,
        “Data”: [true, false, false, true]
      },
    },
    "analog": {
      "input": {
        “StartAddress”: 0,
        “AddressCount”: 3,
        “Data”: [1, 2, 3]
      }
    },
  }
}

#### Write mts-io data response

	The json response will resemble the following:
	
{
  “success”: true|false,
  “error”: “the error message”,
  “timestamp”: the_timestamp,
  “action”: “write”,
  "gpio": {
    "digital": {
      “StartAddress”: 0,
      “AddressCount”: 4,
      “Data”: [true, false, false, true]
    },
    "analog": {
      “StartAddress”: 0,
      “AddressCount”: 4,
      “Data”: [1, 2, 3, 4]
    },
  }
}

## ClearBlade Platform Dependencies
The mtacGpioAdapter adapter was constructed to provide the ability to communicate with a _System_ defined in a ClearBlade Platform instance. Therefore, the adapter requires a _System_ to have been created within a ClearBlade Platform instance.

Once a System has been created, artifacts must be defined within the ClearBlade Platform system to allow the adapters to function properly. At a minimum: 

  * A device needs to be created in the Auth --> Devices collection. The device will represent the adapter, or more importantly, the device or MultiTech Conduit gateway on which the adapter is executing. The _name_ and _active key_ values specified in the Auth --> Devices collection will be used by the adapter to authenticate to the ClearBlade Platform or ClearBlade Edge. 
  * An adapter configuration data collection needs to be created in the ClearBlade Platform _system_ and populated with the data appropriate to the mts-io adapter. The schema of the data collection should be as follows:


| Column Name      | Column Datatype |
| ---------------- | --------------- |
| adapter_name     | string          |
| topic_root       | string          |
| adapter_settings | string (json)   |

### adapter_settings
The adapter_settings column is not used in the __mtacGpioAdapter__ Adapter

## Usage

### Executing the adapter

`mtacGpioAdapter -systemKey=<SYSTEM_KEY> -systemSecret=<SYSTEM_SECRET> -platformURL=<PLATFORM_URL> -messagingURL=<MESSAGING_URL> -deviceName=<DEVICE_NAME> -password=<DEVICE_ACTIVE_KEY> -adapterConfigCollectionID=<COLLECTION_ID> -logLevel=<LOG_LEVEL>`

   __*Where*__ 

   __systemKey__
  * REQUIRED
  * The system key of the ClearBLade Platform __System__ the adapter will connect to

   __systemSecret__
  * REQUIRED
  * The system secret of the ClearBLade Platform __System__ the adapter will connect to
   
   __deviceName__
  * The device name the adapter will use to authenticate to the ClearBlade Platform
  * Requires the device to have been defined in the _Auth - Devices_ collection within the ClearBlade Platform __System__
  * OPTIONAL
  * Defaults to __mtacGpioAdapter__
   
   __password__
  * REQUIRED
  * The active key the adapter will use to authenticate to the platform
  * Requires the device to have been defined in the _Auth - Devices_ collection within the ClearBlade Platform __System__
   
   __platformUrl__
  * The url of the ClearBlade Platform instance the adapter will connect to
  * OPTIONAL
  * Defaults to __http://localhost:9000__

   __messagingUrl__
  * The MQTT url of the ClearBlade Platform instance the adapter will connect to
  * OPTIONAL
  * Defaults to __localhost:1883__

   __adapterConfigCollectionID__
  * REQUIRED 
  * The collection ID of the data collection used to house adapter configuration data

   __logLevel__
  * The level of runtime logging the adapter should provide.
  * Available log levels:
    * fatal
    * error
    * warn
    * info
    * debug
  * OPTIONAL
  * Defaults to __info__


## Setup
---
The mtsIo adapter is dependent upon the ClearBlade Go SDK and its dependent libraries being installed. The mtsIo adapter was written in Go and therefore requires Go to be installed (https://golang.org/doc/install).

In addition, the adapter is dependent upon the Multitech C/C++ toolchain being installed. MultiTech Conduit gateways currently execute a specialized ARM 5 version of Linux named mLinux. Compiling the adapter and utilizing the C/C++ toolchain requires development to be done within a 64-bit Linux OS.

 * These steps require a PC running a 64-bit Linux OS or a virtual machine that runs a 64-bit Linux OS. For mLinux 3, Ubuntu 14.04 is preferred.  For mLinux 4, Ubuntu 14.04 or Ubuntu 16.04 may be used.


### Installing the Multitech C/C++ toolchain
Information regarding the C/C++ toolchain and its installation can be found at http://www.multitech.net/developer/software/mlinux/mlinux-software-development/mlinux-c-toolchain/

The following steps summarize the installation of the MultiTech C/C++ toolchain:

 1. Download the installation script  
     * ```curl http://www.multitech.net/mlinux/sdk/3.3.6/mlinux-eglibc-x86_64-mlinux-factory-image-arm926ejste-toolchain-3.3.6.sh > mlinux-toolchain-install.sh```
 2. Install the installation script  
    * ```chmod +x mlinux-toolchain-install.sh```
    * ```./mlinux-toolchain-install.sh```
 3. Load the toolchain environment  
    * ```source /path/to/sdk/environment-setup-arm926ejste-mlinux-linux-gnueabi```

### Adapter compilation
In order to compile the adapter for execution within mLinux, the following steps need to be performed:

 1. Retrieve the adapter source code  
    * ```git clone git@github.com:ClearBlade/Multitech-MTSIO-Adapter.git```
 2. Navigate to the _mtsioadapter_ directory  
    * ```cd mtsioadapter```
 3. Load the toolchain environment - If this hasn't already been done  
    * ```source /path/to/sdk/environment-setup-arm926ejste-mlinux-linux-gnueabi```
 4. Compile the adapter
    * ```GOARCH=arm GOARM=5 CGO_ENABLED=1 GOOS=linux go build```



