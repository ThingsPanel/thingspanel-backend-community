package service

import (
	"encoding/hex"
	"strings"
	"time"

	dal "project/dal"
	global "project/global"
	initialize "project/initialize"
	model "project/model"
	utils "project/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type DataScript struct{}

func DelDataScriptCache(data_script *model.DataScript) error {
	deviceIDs, err := dal.GetDeviceIDsByDataScriptID(data_script.ID)
	if err != nil {
		logrus.Error(err)
		return err
	}

	for _, deviceID := range deviceIDs {
		if data_script.ScriptType == "A" {
			_ = global.REDIS.Del(deviceID + "_telemetry_script_flag").Err()
			_ = global.REDIS.Del(deviceID + "_" + data_script.ScriptType + "_script").Err()
		} else {
			_ = global.REDIS.Del(deviceID + "_" + data_script.ScriptType + "_script").Err()
		}
	}
	return nil
}

func (p *DataScript) CreateDataScript(req *model.CreateDataScriptReq) (data_script model.DataScript, err error) {

	data_script.ID = uuid.New()
	data_script.Name = req.Name
	data_script.Description = req.Description
	data_script.DeviceConfigID = req.DeviceConfigId
	data_script.EnableFlag = "N"
	data_script.Content = req.Content
	data_script.ScriptType = req.ScriptType
	data_script.LastAnalogInput = req.LastAnalogInput

	t := time.Now().UTC()
	data_script.CreatedAt = &t
	data_script.UpdatedAt = &t

	data_script.Remark = req.Remark
	err = dal.CreateDataScript(&data_script)

	if err != nil {
		logrus.Error(err)
	}

	return data_script, err
}

func (p *DataScript) UpdateDataScript(UpdateDataScriptReq *model.UpdateDataScriptReq) error {

	err := dal.UpdateDataScript(UpdateDataScriptReq)
	if err != nil {
		logrus.Error(err)
		return err
	}

	new_script, _ := dal.GetDataScriptById(UpdateDataScriptReq.Id)
	err = DelDataScriptCache(new_script)
	if err != nil {
		logrus.Error(err)
		return err
	}

	return err
}

func (p *DataScript) DeleteDataScript(id string) error {
	new_script, err := dal.GetDataScriptById(id)
	if err != nil {
		logrus.Error(err)
		return err
	}

	err = dal.DeleteDataScript(id)
	if err != nil {
		logrus.Error(err)
		return err
	}
	if new_script.EnableFlag == "Y" {
		_ = DelDataScriptCache(new_script)
	}
	return err
}

func (p *DataScript) GetDataScriptListByPage(Params *model.GetDataScriptListByPageReq) (map[string]interface{}, error) {

	total, list, err := dal.GetDataScriptListByPage(Params)
	if err != nil {
		return nil, err
	}
	data_scriptListRsp := make(map[string]interface{})
	data_scriptListRsp["total"] = total
	data_scriptListRsp["list"] = list

	return data_scriptListRsp, err
}

func (p *DataScript) QuizDataScript(req *model.QuizDataScriptReq) (string, error) {
	if strings.HasPrefix(req.AnalogInput, "0x") {
		msg, err := hex.DecodeString(strings.ReplaceAll(req.AnalogInput, "0x", ""))
		if err != nil {
			return "", err
		}
		return utils.ScriptDeal(req.Content, msg, req.Topic)
	}

	return utils.ScriptDeal(req.Content, []byte(req.AnalogInput), req.Topic)

}

func (p *DataScript) EnableDataScript(req *model.EnableDataScriptReq) error {

	if req.EnableFlag == "Y" {
		if ok, err := dal.OnlyOneScriptTypeEnabled(req.Id); !ok {
			return err
		}
	}

	var data_script model.DataScript
	data_script.ID = req.Id
	data_script.EnableFlag = req.EnableFlag

	err := dal.EnableDataScript(&data_script)
	if err != nil {
		logrus.Error(err)
		return err
	}

	if req.EnableFlag == "N" {
		new_script, _ := dal.GetDataScriptById(req.Id)
		err = DelDataScriptCache(new_script)
		if err != nil {
			logrus.Error(err)
			return err
		}
	}

	return err
}

func (p *DataScript) Exec(device *model.Device, scriptType string, msg []byte, topic string) ([]byte, error) {
	var script_id string
	var err error
	if scriptType == "A" {
		script_id, err = initialize.GetTelemetryScriptFlagByDeviceAndScriptType(device, scriptType)
		if err != nil {
			return msg, err
		}
		if script_id == "" {
			return msg, nil
		}
	}

	script, err := initialize.GetScriptByDeviceAndScriptType(device, scriptType)
	if err != nil {
		return msg, err
	}
	if script == nil {
		return msg, nil
	}
	newMsg, err := utils.ScriptDeal(*script.Content, msg, topic)
	if err != nil {
		return msg, err
	}
	return []byte(newMsg), nil
}
