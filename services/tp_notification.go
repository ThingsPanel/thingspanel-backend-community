package services

import (
	"ThingsPanel-Go/initialize/psql"
	sendmessage "ThingsPanel-Go/initialize/send_message"
	"ThingsPanel-Go/models"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beego/beego/v2/core/logs"
)

type TpNotificationService struct {
}

func SaveNotification(ng models.TpNotificationGroups, nm []models.TpNotificationMembers) bool {

	result := psql.Mydb.Save(ng)
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

	err = tx.Limit(pageSize).Offset(offset).Find(&nG).Error
	if err != nil {
		logs.Error(err.Error())
		return nG, count
	}
	return nG, count
}

func (*TpNotificationService) ExecuteNotification(strategyId string) {

	// 通过策略ID ，获取info_way中的信息
	var WarningStrategyService TpWarningStrategyService
	StrategyDetail, _ := WarningStrategyService.GetTpWarningStrategyDetail(strategyId)
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
			fmt.Println("members", members)
			for _, v2 := range members {
				// 查询每一个用户
				user, cnt := UsersService.GetUserById(v2.UsersId)
				if cnt != 0 {
					if v2.IsEmail == 1 {
						// 发送邮件
						sendmessage.SendEmailMessage("告警邮件测试发送", "测试内容", user.Email)
					}
				}
			}

		case models.NotificationType_Email:
			// 解析config
			fmt.Println(v.NotificationConfig)
			nConfig := make(map[string]string)
			err := json.Unmarshal([]byte(v.NotificationConfig), &nConfig)
			if err != nil {
				continue
			}
			emailList := strings.Split(nConfig["email"], ",")
			for _, ev := range emailList {
				sendmessage.SendEmailMessage("告警邮件测试发送", "测试内容", ev)
			}
		case models.NotificationType_Webhook:
			// 解析config
			fmt.Println(v.NotificationConfig)
			nConfig := make(map[string]string)
			err := json.Unmarshal([]byte(v.NotificationConfig), &nConfig)
			if err != nil {
				continue
			}
			webhookList := strings.Split(nConfig["webhook"], ",")
			fmt.Println("emailList", webhookList)
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
