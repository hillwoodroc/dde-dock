/**
 * Copyright (C) 2014 Deepin Technology Co., Ltd.
 *
 * This program is free software; you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation; either version 3 of the License, or
 * (at your option) any later version.
 **/

package power

import (
	libdisplay "dbus/com/deepin/daemon/display"
	libkeybinding "dbus/com/deepin/daemon/keybinding"
	libsessionwatcher "dbus/com/deepin/daemon/sessionwatcher"
	liblockfront "dbus/com/deepin/dde/lockfront"
	libsessionmanager "dbus/com/deepin/sessionmanager"
	libnotifications "dbus/org/freedesktop/notifications"
	libscreensaver "dbus/org/freedesktop/screensaver"
	libupower "dbus/org/freedesktop/upower"

	"fmt"
	"gir/gio-2.0"
	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/dpms"
	"github.com/BurntSushi/xgbutil"
	"pkg.deepin.io/lib/dbus"
	"pkg.deepin.io/lib/dbus/property"
	"pkg.deepin.io/lib/initializer/v2"
)

var submoduleList = []func(*Manager) (string, submodule, error){}

type Manager struct {
	mediaKey       *libkeybinding.Mediakey
	notifier       *libnotifications.Notifier
	sessionManager *libsessionmanager.SessionManager
	sessionWatcher *libsessionwatcher.SessionWatcher
	screenSaver    *libscreensaver.ScreenSaver
	display        *libdisplay.Display
	lockFront      *liblockfront.LockFront
	upower         *libupower.Upower

	xConn        *xgb.Conn
	xu           *xgbutil.XUtil
	batteryGroup *batteryGroup
	submodules   map[string]submodule

	isSuspending           bool
	batteryPowerLevel      uint32
	settings               *gio.Settings
	powerLevelTicker       *countTicker
	usePercentageForPolicy bool

	timeToEmptyLow       int64
	timeToEmptyVeryLow   int64
	timeToEmptyExhausted int64

	batteryPercentageLow       float64
	batteryPercentageVeryLow   float64
	batteryPercentageExhausted float64

	powerSupplyDataBackend int

	// 接通电源时，不做任何操作，到关闭屏幕需要的时间
	LinePowerScreenBlackDelay *property.GSettingsIntProperty `access:"readwrite"`
	// 接通电源时，不做任何操作，从黑屏到睡眠的时间
	LinePowerSleepDelay *property.GSettingsIntProperty `access:"readwrite"`

	// 使用电池时，不做任何操作，到关闭屏幕需要的时间
	BatteryScreenBlackDelay *property.GSettingsIntProperty `access:"readwrite"`
	// 使用电池时，不做任何操作，从黑屏到睡眠的时间
	BatterySleepDelay *property.GSettingsIntProperty `access:"readwrite"`

	// 关闭显示器前是否锁定
	ScreenBlackLock *property.GSettingsBoolProperty `access:"readwrite"`
	// 睡眠前是否锁定
	SleepLock *property.GSettingsBoolProperty `access:"readwrite"`

	// 按下电源按钮后执行的命令
	PowerButtonAction *property.GSettingsStringProperty `access:"readwrite"`
	// 笔记本电脑关闭盖子后执行的命令
	LidClosedAction *property.GSettingsStringProperty `access:"readwrite"`

	// 是否有盖子，一般笔记本电脑才有
	LidIsPresent bool
	// 是否使用电池, 接通电源时为 false, 使用电池时为 true
	OnBattery bool

	// 电池是否可用，是否存在
	BatteryIsPresent map[string]bool
	// 电池电量百分比
	BatteryPercentage map[string]float64
	// 电池状态
	BatteryState map[string]uint32
}

type submodule interface {
	Start() error
	Destroy()
}

func (m *Manager) initDBusLib() error {
	var err error
	m.mediaKey, err = libkeybinding.NewMediakey("com.deepin.daemon.Keybinding", "/com/deepin/daemon/Keybinding/Mediakey")
	if err != nil {
		logger.Error("init mediaKey failed:", err)
		return err
	}

	logger.Debugf("upower dest: %q, upower path: %q", upowerDBusDest, upowerDBusObjPath)
	m.upower, err = libupower.NewUpower(upowerDBusDest, dbus.ObjectPath(upowerDBusObjPath))
	if err != nil {
		logger.Warning("init upower failed:", err)
		return err
	}

	m.notifier, err = libnotifications.NewNotifier("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	if err != nil {
		logger.Error("init notifier failed:", err)
		return err
	}

	m.sessionManager, err = libsessionmanager.NewSessionManager("com.deepin.SessionManager", "/com/deepin/SessionManager")
	if err != nil {
		logger.Error("init sessionManager failed:", err)
		return err
	}

	m.screenSaver, err = libscreensaver.NewScreenSaver("org.freedesktop.ScreenSaver", "/org/freedesktop/ScreenSaver")
	if err != nil {
		logger.Error("init screenSaver failed:", err)
		return err
	}

	m.display, err = libdisplay.NewDisplay(dbusDisplayDest, dbusDisplayPath)
	if err != nil {
		logger.Error("init display failed:", err)
		return err
	}

	m.lockFront, err = liblockfront.NewLockFront("com.deepin.dde.lockFront", "/com/deepin/dde/lockFront")
	if err != nil {
		logger.Error("init lockFront failed:", err)
		return err
	}

	m.sessionWatcher, err = libsessionwatcher.NewSessionWatcher("com.deepin.daemon.SessionWatcher", "/com/deepin/daemon/SessionWatcher")
	if err != nil {
		logger.Error("init SessionWatcher failed:", err)
		return err
	}

	return nil
}

func (m *Manager) finalizeDBusLib() {
	if m.mediaKey != nil {
		libkeybinding.DestroyMediakey(m.mediaKey)
		m.mediaKey = nil
	}

	if m.upower != nil {
		libupower.DestroyUpower(m.upower)
		m.upower = nil
	}

	if m.notifier != nil {
		libnotifications.DestroyNotifier(m.notifier)
		m.notifier = nil
	}

	if m.sessionManager != nil {
		libsessionmanager.DestroySessionManager(m.sessionManager)
		m.sessionManager = nil
	}

	if m.screenSaver != nil {
		libscreensaver.DestroyScreenSaver(m.screenSaver)
		m.screenSaver = nil
	}

	if m.display != nil {
		libdisplay.DestroyDisplay(m.display)
		m.display = nil
	}

	if m.lockFront != nil {
		m.lockFront = nil
	}

	if m.sessionWatcher != nil {
		libsessionwatcher.DestroySessionWatcher(m.sessionWatcher)
		m.sessionWatcher = nil
	}
}

func NewManager() (*Manager, error) {
	logger.Debug("NewManager")
	m := &Manager{}

	m.settings = gio.NewSettings("com.deepin.dde.power")
	m.powerSupplyDataBackend = powerSupplyDataBackendUPower
	m.usePercentageForPolicy = m.settings.GetBoolean(settingKeyUsePercentageForPolicy)

	m.batteryPercentageLow = float64(m.settings.GetInt(settingKeyPercentageLow))
	m.batteryPercentageVeryLow = float64(m.settings.GetInt(settingKeyPercentageVeryLow))
	m.batteryPercentageExhausted = float64(m.settings.GetInt(settingKeyPercentageExhausted))

	m.timeToEmptyLow = int64(m.settings.GetInt(settingKeyTimeToEmptyLow))
	m.timeToEmptyVeryLow = int64(m.settings.GetInt(settingKeyTimeToEmptyVeryLow))
	m.timeToEmptyExhausted = int64(m.settings.GetInt(settingKeyTimeToEmptyExhausted))

	m.LinePowerScreenBlackDelay = property.NewGSettingsIntProperty(m, "LinePowerScreenBlackDelay", m.settings, settingKeyLinePowerScreenBlackDelay)
	m.LinePowerSleepDelay = property.NewGSettingsIntProperty(m, "LinePowerSleepDelay", m.settings, settingKeyLinePowerSleepDelay)
	m.BatteryScreenBlackDelay = property.NewGSettingsIntProperty(m, "BatteryScreenBlackDelay", m.settings, settingKeyBatteryScreenBlackDelay)
	m.BatterySleepDelay = property.NewGSettingsIntProperty(m, "BatterySleepDelay", m.settings, settingKeyBatterySleepDelay)

	m.ScreenBlackLock = property.NewGSettingsBoolProperty(m, "ScreenBlackLock", m.settings, settingKeyScreenBlackLock)
	m.SleepLock = property.NewGSettingsBoolProperty(m, "SleepLock", m.settings, settingKeySleepLock)

	m.PowerButtonAction = property.NewGSettingsStringProperty(m, "PowerButtonAction", m.settings, settingKeyPowerButtonPressedExec)
	m.LidClosedAction = property.NewGSettingsStringProperty(m, "LidClosedAction", m.settings, settingKeyLidClosedExec)

	err := initializer.Do(m.initDBusLib).Do(
		func() error {
			return dbus.InstallOnSession(m)
		}).Do(
		m.initBatteryGroup).Do(
		m.initXConn).Do(
		m.initSubmodules).GetError()

	if err != nil {
		m.destroy()
		return nil, err
	}

	m.initPowerModule()

	// start all submodule
	for name, submodule := range m.submodules {
		logger.Debug("Start submodule:", name)
		submodule.Start()
	}

	m.initEventHandle()
	return m, nil
}

func (m *Manager) initXConn() error {
	var err error
	m.xConn, err = xgb.NewConn()
	if err != nil {
		return err
	}
	m.xu, err = xgbutil.NewConnXgb(m.xConn)
	if err != nil {
		return err
	}
	dpms.Init(m.xConn)
	return nil
}

func (m *Manager) initSubmodules() error {
	m.submodules = make(map[string]submodule, len(submoduleList))
	// new all submodule
	for _, newMethod := range submoduleList {
		name, submoduleInstance, err := newMethod(m)
		logger.Debug("New submodule:", name)
		if err != nil {
			return err
		}
		m.submodules[name] = submoduleInstance
	}
	return nil
}

func (m *Manager) getSubmodule(name string) (submodule, error) {
	module, ok := m.submodules[name]
	if !ok {
		return nil, fmt.Errorf("no submodule: %v", name)
	}
	return module, nil
}

func (m *Manager) destroy() {
	m.finalizeDBusLib()
	dbus.UnInstallObject(m)

	if m.submodules != nil {
		for name, submodule := range m.submodules {
			logger.Debug("destroy submodule:", name)
			submodule.Destroy()
		}
		m.submodules = nil
	}

	if m.batteryGroup != nil {
		m.batteryGroup.Destroy()
		m.batteryGroup = nil
	}

	if m.xConn != nil {
		m.xConn.Close()
		m.xConn = nil
	}
}

func (m *Manager) initProperties() {
	upower := m.upower
	m.setPropOnBattery(upower.OnBattery.Get())
	m.setPropLidIsPresent(upower.LidIsPresent.Get())

	m.BatteryIsPresent = make(map[string]bool)
	m.BatteryPercentage = make(map[string]float64)
	m.BatteryState = make(map[string]uint32)
}

func (m *Manager) initBatteryGroup() error {
	logger.Debug("initBatteryGroup")
	m.initProperties()

	var err error
	m.batteryGroup, err = NewBatteryGroup(m)
	if err != nil {
		logger.Error(err)
		return err
	}
	if m.batteryGroup.batteryDevicesCount() > 0 {
		m.setPropBatteryIsPresent(batteryDisplay, true)
	}
	m.checkBatteryPowerLevel(m.batteryGroup)
	return nil
}

func (m *Manager) initPowerModule() {
	inited := m.settings.GetBoolean(settingKeyPowerModuleInitialized)
	if !inited {
		// TODO: 也许有更好的判断台式机的方法
		if m.batteryGroup.batteryDevicesCount() == 0 {
			// 电池数量为零，判断为台式机, 设置待机为 从不
			m.LinePowerSleepDelay.Set(0)
			m.BatterySleepDelay.Set(0)
		}

		m.settings.SetBoolean(settingKeyPowerModuleInitialized, true)
	}
}

func (m *Manager) getNewBatteryPowerLevel(batGroup *batteryGroup) uint32 {
	if !m.OnBattery {
		return batteryPowerLevelSufficient
	}

	return batGroup.getPowerLevel(m.usePercentageForPolicy)
}

func (m *Manager) setPropLidIsPresent(val bool) {
	m.LidIsPresent = val
	dbus.NotifyChange(m, "LidIsPresent")
}

func (m *Manager) setPropOnBattery(val bool) {
	m.OnBattery = val
	dbus.NotifyChange(m, "OnBattery")
}

func (m *Manager) setPropBatteryIsPresent(path string, isPresent bool) {
	m.BatteryIsPresent[path] = isPresent
	dbus.NotifyChange(m, "BatteryIsPresent")
}

func (m *Manager) setPropBatteryPercentage(path string, p float64) {
	m.BatteryPercentage[path] = p
	dbus.NotifyChange(m, "BatteryPercentage")
}

func (m *Manager) setPropBatteryState(path string, state batteryStateType) {
	m.BatteryState[path] = uint32(state)
	dbus.NotifyChange(m, "BatteryState")
}

func (m *Manager) Reset() {
	logger.Debug("Reset settings")

	var settingKeys = []string{
		settingKeyLinePowerScreenBlackDelay,
		settingKeyLinePowerSleepDelay,
		settingKeyBatteryScreenBlackDelay,
		settingKeyBatterySleepDelay,
		settingKeyScreenBlackLock,
		settingKeySleepLock,
		settingKeyLidClosedExec,
		settingKeyPowerButtonPressedExec,
	}
	for _, key := range settingKeys {
		logger.Debug("reset setting", key)
		m.settings.Reset(key)
	}
}
