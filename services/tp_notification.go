package services

import (
	"ThingsPanel-Go/initialize/psql"
	sendmessage "ThingsPanel-Go/initialize/send_message"
	"ThingsPanel-Go/models"
	tphttp "ThingsPanel-Go/others/http"
	"ThingsPanel-Go/utils"
	"encoding/json"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpNotificationService struct {
}

func SaveNotification(ng models.TpNotificationGroups, nm []models.TpNotificationMembers, isCreate bool) bool {

	var result *gorm.DB
	if !isCreate {
		result = psql.Mydb.Omit("create_time").Save(ng)
	} else {
		result = psql.Mydb.Save(ng)
	}

	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}

	delres := DeleteNotificationMembers(ng.Id)

	if !delres {
		return false
	}

	if len(nm) > 0 {
		result = psql.Mydb.Create(&nm)
		if result.Error != nil {
			logs.Error(result.Error.Error())
			return false
		}
	}

	return true
}

func GetNotificationGroupsStatus(id string) int {
	var res models.TpNotificationGroups
	result := psql.Mydb.Where("id = ?", id).Find(&res)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return -1
	}
	return res.Status
}

func GetNotificationGroups(id string) models.TpNotificationGroups {
	var res models.TpNotificationGroups
	result := psql.Mydb.Where("id = ?", id).Find(&res)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return res
	}
	return res
}

func GetNotificationMenbers(id string) []models.TpNotificationMembers {
	var res []models.TpNotificationMembers
	tx := psql.Mydb.Model(&models.TpNotificationMembers{})
	err := tx.Where("tp_notification_groups_id = ?", id).Find(&res).Error
	if err != nil {
		logs.Error(err.Error())
		return res
	}
	return res
}

func UpdateNotificationGroupsStatus(id string, s int) bool {
	tx := psql.Mydb.Model(&models.TpNotificationGroups{})
	err := tx.Where("id = ?", id).Update("status", s).Error
	if err != nil {
		logs.Error(err.Error())
		return false
	}
	return true
}

func DeleteNotificationGroups(id string) bool {
	result := psql.Mydb.Where("id = ?", id).Delete(&models.TpNotificationGroups{})
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}
	return true
}

func DeleteNotificationMembers(id string) bool {
	if len(id) > 0 {
		result := psql.Mydb.Where("tp_notification_groups_id = ?", id).Delete(&models.TpNotificationMembers{})
		if result.Error != nil {
			logs.Error(result.Error.Error())
			return false
		}
	}
	return true
}

func GetNotificationListByTenantId(
	offset int, pageSize int, tenantId string) ([]models.TpNotificationGroups, int64) {

	var nG []models.TpNotificationGroups
	var count int64

	tx := psql.Mydb.Model(&models.TpNotificationGroups{})
	tx.Where("tenant_id = ?", tenantId)

	err := tx.Count(&count).Error
	if err != nil {
		logs.Error(err.Error())
		return nG, count
	}

	err = tx.Limit(pageSize).Offset(offset).Order("create_time desc").Find(&nG).Error
	if err != nil {
		logs.Error(err.Error())
		return nG, count
	}
	return nG, count
}

func (*TpNotificationService) ExecuteNotification(strategyId, tenantId, title, content string, countSwitch bool, log string) {

	// 通过策略ID ，获取info_way中的信息
	var WarningStrategyService TpWarningStrategyService
	StrategyDetail, _ := WarningStrategyService.GetTpWarningStrategyDetail(strategyId)

	// 临时增加开关，因为自动化那边本身带有计数，后续统一收敛到这里计数
	if countSwitch {
		// 计数
		if StrategyDetail.RepeatCount+1 >= StrategyDetail.TriggerCount {
			// 达到次数，次数清零。
			result := psql.Mydb.Model(&models.TpWarningStrategy{}).Where("id = ?", strategyId).Update("trigger_count", 0)
			if result.Error != nil {
				logs.Error(result.Error)
			}
		} else {
			// 为达到次数，次数+1，退出
			result := psql.Mydb.Model(&models.TpWarningStrategy{}).Where("id = ?", strategyId).Update("trigger_count", StrategyDetail.TriggerCount+1)
			if result.Error != nil {
				logs.Error(result.Error)
			}
			return
		}
	}

	// 解析InformWay，可以是多个告警组ID
	infoWayList := strings.Split(StrategyDetail.InformWay, ",")

	// 未配置告警组
	if len(infoWayList) == 0 {
		return
	}

	groupList, err := BatchGetNotificationGroups(infoWayList)
	if err != nil || len(groupList) == 0 {
		return
	}

	var UsersService UserService
	// 向每一组发送通知
	for _, v := range groupList {

		switch v.NotificationType {
		case models.NotificationType_Members:
			// 继续查询members表
			members := GetNotificationMenbers(v.Id)
			for _, v2 := range members {
				// 查询每一个用户
				user, cnt := UsersService.GetUserById(v2.UsersId)
				if cnt != 0 {
					if v2.IsEmail == 1 {
						// 发送邮件
						sendmessage.SendEmailMessage(title, content, tenantId, user.Email)
					}
				}
			}

		case models.NotificationType_Email:
			// 解析config
			nConfig := make(map[string]string)
			err := json.Unmarshal([]byte(v.NotificationConfig), &nConfig)
			if err != nil {
				continue
			}
			emailList := strings.Split(nConfig["email"], ",")
			for _, ev := range emailList {
				sendmessage.SendEmailMessage(title, content, tenantId, ev)
			}
		case models.NotificationType_Webhook: // TODO: webhook，发送参数自己定义，一般是告警信息的json，包含告警级别，告警内容，告警时间，告警详情等
			// 解析config
			nConfig := make(map[string]string)
			err := json.Unmarshal([]byte(v.NotificationConfig), &nConfig)
			if err != nil {
				continue
			}
			webs := strings.Split(nConfig["webhook"], ",")
			info := make(map[string]string)
			info["alert_level"] = StrategyDetail.WarningLevel
			info["alert_description"] = StrategyDetail.WarningDescription
			info["alert_time"] = time.Now().Format("2006-01-02T15:04:05Z")
			info["alert_details"] = log
			infoByte, _ := json.Marshal(info)
			for _, target := range webs {
				tphttp.PostJson(target, infoByte)
			}
		// TODO: 企业微信群机器人/钉钉群机器人/飞书群机器人
		// 需要确定这些机器人的接口，以及接口的参数
		default:

			return
		}
	}

}

// 查询当前启用的告警组
func BatchGetNotificationGroups(id []string) ([]models.TpNotificationGroups, error) {
	var groupInfo []models.TpNotificationGroups
	err := psql.Mydb.
		Model(&models.TpNotificationGroups{}).
		Where("id IN (?) AND status = ?", id, models.NotificationSwitch_Open).
		Find(&groupInfo).Error
	if err != nil {
		return groupInfo, err
	}
	return groupInfo, err
}

func (*TpNotificationService) SaveNotificationConfigAli(noticeType int, config models.CloudServicesConfig_Ali, status int) (err error) {

	exists, err := GetThirdPartyCloudServicesConfigByNoticeType(noticeType)
	if err != nil {
		return err
	}
	configStr, err := json.Marshal(config)
	if len(exists) == 0 {
		if err != nil {
			return err
		}
		t := models.ThirdPartyCloudServicesConfig{
			Id:         utils.GetUuid(),
			NoticeType: noticeType,
			Config:     string(configStr),
			Status:     status,
		}
		result := psql.Mydb.Save(t)
		if result.Error != nil {
			return result.Error
		}
	} else {
		for _, v := range exists {
			var tmp models.CloudServicesConfig_Ali
			err = json.Unmarshal([]byte(v.Config), &tmp)
			if err != nil {
				return err
			}
			if tmp.CloudType == models.NotificationCloudType_Ali {
				t := models.ThirdPartyCloudServicesConfig{
					Id:         v.Id,
					NoticeType: noticeType,
					Config:     string(configStr),
					Status:     status,
				}
				result := psql.Mydb.Save(t)
				if result.Error != nil {
					return result.Error
				}
				break
			}
		}
	}

	return err
}

func (*TpNotificationService) SaveNotificationConfigEmail(config models.CloudServicesConfig_Email, status int) (err error) {

	exists, err := GetThirdPartyCloudServicesConfigByNoticeType(models.NotificationConfigType_Email)
	if err != nil {
		return err
	}
	configStr, err := json.Marshal(config)
	if len(exists) == 0 {
		if err != nil {
			return err
		}
		t := models.ThirdPartyCloudServicesConfig{
			Id:         utils.GetUuid(),
			NoticeType: models.NotificationType_Email,
			Config:     string(configStr),
			Status:     status,
		}
		result := psql.Mydb.Save(t)
		if result.Error != nil {
			return result.Error
		}
	} else {
		for _, v := range exists {
			var tmp models.CloudServicesConfig_Email
			err = json.Unmarshal([]byte(v.Config), &tmp)
			if err != nil {
				return err
			}

			t := models.ThirdPartyCloudServicesConfig{
				Id:         v.Id,
				NoticeType: models.NotificationType_Email,
				Config:     string(configStr),
				Status:     status,
			}
			result := psql.Mydb.Save(t)
			if result.Error != nil {
				return result.Error
			}
			break
		}
	}

	return err
}

func GetThirdPartyCloudServicesConfigByNoticeType(noticeType int) (res []models.ThirdPartyCloudServicesConfig, err error) {
	err = psql.Mydb.
		Model(&models.ThirdPartyCloudServicesConfig{}).
		Where("notice_type = ?", noticeType).
		Find(&res).Error
	if err != nil {
		return res, err
	}
	return res, err
}

func (*TpNotificationService) GetNotificationHistory(
	Offset, PerPage, NotificationType int, ReceiveTarget string, StartTime, EndTime int64, TenantId string,
) (d []models.TpNotificationHistory, count int64) {

	where := make(map[string]interface{})

	where["tenant_id"] = TenantId

	if NotificationType != 0 {
		where["notification_type"] = NotificationType
	}
	if ReceiveTarget != "" {
		where["send_target"] = ReceiveTarget
	}
	if StartTime > 0 {
		where["start_time >= ?"] = StartTime
	}
	if EndTime > 0 {
		where["end_time <= ?"] = EndTime
	}

	tx := psql.Mydb.Model(&models.TpNotificationHistory{})
	tx.Where(where)

	err := tx.Count(&count).Error
	if err != nil {
		logs.Error(err.Error())
		return d, count
	}

	err = tx.Limit(PerPage).Offset(Offset).Order("send_time desc").Find(&d).Error
	if err != nil {
		logs.Error(err.Error())
		return d, count
	}
	return d, count

}
