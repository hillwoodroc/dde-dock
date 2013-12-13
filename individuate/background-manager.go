/**
 * Copyright (c) 2011 ~ 2013 Deepin, Inc.
 *               2011 ~ 2013 jouyouyun
 *
 * Author:      jouyouyun <jouyouwen717@gmail.com>
 * Maintainer:  jouyouyun <jouyouwen717@gmail.com>
 *
 * This program is free software; you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation; either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program; if not, see <http://www.gnu.org/licenses/>.
 *
 * Function: Manager Background switch/add/delete etc...
 **/

package main

import (
	"dlib/dbus/property"
	"dlib/gio-2.0"
	/*"fmt"*/)

type BackgroundManager struct {
	AutoSwitch     *property.GSettingsBoolProperty   `access:"readwrite"`
	SwitchDuration *property.GSettingsIntProperty    `access:"readwrite"`
	CrossFadeMode  *property.GSettingsStringProperty `access:"readwrite"`
	CurrentPicture *property.GSettingsStringProperty
	PictureURIS    *property.GSettingsStrvProperty
	PictureIndex   *property.GSettingsIntProperty
}

const (
	INDIVIDUATE_ID = "com.deepin.dde.individuate"
)

var (
	indiviGSettings = gio.NewSettings(INDIVIDUATE_ID)
)

func (bgManager *BackgroundManager) SetBackgroundPicture(path string, replace bool) {
	pictStrv := []string{}
	if replace {
		/* use 'path' replace 'PictureURIS' */
		pictStrv = append(pictStrv, path)
		indiviGSettings.SetInt("index", 0)
	} else {
		/* append 'path' to 'PictureURIS' */
		pictStrv = indiviGSettings.GetStrv("picture-uris")
		l := len(pictStrv)
		pictStrv = append(pictStrv, path)
		indiviGSettings.SetInt("index", l)
	}

	indiviGSettings.SetStrv("picture-uris", pictStrv)
}

func NewBackgroundManager() *BackgroundManager {
	bgManager := &BackgroundManager{}

	bgManager.AutoSwitch = property.NewGSettingsBoolProperty(
		bgManager, "AutoSwitch",
		indiviGSettings, "auto-switch")
	bgManager.SwitchDuration = property.NewGSettingsIntProperty(
		bgManager, "SwitchDuration",
		indiviGSettings, "background-duration")
	bgManager.CrossFadeMode = property.NewGSettingsStringProperty(
		bgManager, "CrossFadeMode",
		indiviGSettings, "cross-fade-auto-mode")
	bgManager.CurrentPicture = property.NewGSettingsStringProperty(
		bgManager, "CurrentPicture",
		indiviGSettings, "current-picture")
	bgManager.PictureURIS = property.NewGSettingsStrvProperty(
		bgManager, "PictureURIS",
		indiviGSettings, "picture-uris")
	bgManager.PictureIndex = property.NewGSettingsIntProperty(
		bgManager, "PictureIndex",
		indiviGSettings, "index")

	ListenGSetting(bgManager)

	return bgManager
}

func ListenGSetting(bgManager *BackgroundManager) {
	indiviGSettings.Connect("changed::picture-uris", func(s *gio.Settings, key string) {
		/* generate bg blur picture */
	})

	indiviGSettings.Connect("changed::index", func(s *gio.Settings, key string) {
		i := s.GetInt(key)
		uris := s.GetStrv("picture-uris")
		s.SetString("current-picture", uris[i])
	})
}

func AutoSwitchPicture() {
}
