Instructions for use:

1. Copy mtacGpioAdapter.etc.default file into /etc/default, name the file "mtacGpioAdapter"
2. Copy mtacGpioAdapter.etc.initd file into /etc/init.d, name the file "mtacGpioAdapter"
3. From a terminal prompt, execute the following commands:
	3a. chmod 755 /etc/init.d/mtacGpioAdapter
	3b. chown root:root /etc/init.d/mtacGpioAdapter
	3c. update-rc.d mtacGpioAdapter defaults 85

If you wish to start the adapter, rather than reboot, issue the following command from a terminal prompt:

	/etc/init.d/mtacGpioAdapter start