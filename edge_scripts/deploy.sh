#!/bin/bash

#Copy binary to /usr/local/bin
mv mtacGpioAdapter /usr/bin

#Ensure binary is executable
chmod +x /usr/bin/mtacGpioAdapter

#Set up init.d resources so that mtacGpioAdapter is started when the gateway starts
mv mtacGpioAdapter.etc.initd /etc/init.d/mtacGpioAdapter
mv mtacGpioAdapter.etc.default /etc/default/mtacGpioAdapter

#Ensure init.d script is executable
chmod +x /etc/init.d/mtacGpioAdapter

#Add adapter to log rotate
cat << EOF > /etc/logrotate.d/mtacGpioAdapter.conf
/var/log/mtacGpioAdapter {
    size 10M
    rotate 3
    compress
    copytruncate
    missingok
}
EOF

#Remove mtacGpioAdapter from monit in case it was already there
sed -i '/mtacGpioAdapter.pid/{N;N;N;N;d}' /etc/monitrc

#Add the adapter to monit
sed -i '/#  check process monit with pidfile/i \
  check process mtacGpioAdapter with pidfile \/var\/run\/mtacGpioAdapter.pid \
    start program = "\/etc\/init.d\/mtacGpioAdapter start" with timeout 60 seconds \
    stop program  = "\/etc\/init.d\/mtacGpioAdapter stop" \
    depends on edge \
 ' /etc/monitrc

#restart monit
/etc/init.d/monit restart

#Start the adapter
monit start mtacGpioAdapter

echo "mtacGpioAdapter Deployed"