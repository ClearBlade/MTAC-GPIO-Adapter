#!/bin/bash

#Remove mtacGpioAdapter from monit
sed -i '/mtacGpioAdapter.pid/{N;N;N;N;d}' /etc/monitrc

#Remove the init.d script
rm /etc/init.d/mtacGpioAdapter

#Remove the default variables file
rm /etc/default/mtacGpioAdapter

#Remove the binary
rm /usr/bin/mtacGpioAdapter

#restart monit
/etc/init.d/monit restart

