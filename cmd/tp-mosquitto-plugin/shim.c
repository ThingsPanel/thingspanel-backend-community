//go:build mosquitto_plugin

/* Only broker callbacks call Mosquitto APIs. Workers retain integer handles and
 * copied bytes; no Go pointer crosses the ABI or dereferences a client. */
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <mosquitto.h>
#include "_cgo_export.h"

MOSQUITTO_PLUGIN_DECLARE_VERSION(5);
static mosquitto_plugin_id_t *plugin_id;
static int auth_cb(int event,void *data,void *unused){
 (void)event;(void)unused;
 struct mosquitto_evt_basic_auth *e=data;
 if(!e->username || !TPBegin((uintptr_t)e->client,(char*)mosquitto_client_id(e->client),e->username,e->password,(char*)mosquitto_client_address(e->client),mosquitto_client_protocol_version(e->client)))return MOSQ_ERR_AUTH;
 return MOSQ_ERR_AUTH_DELAYED;
}
static int acl_cb(int event,void *data,void *unused){
 (void)event;(void)unused;struct mosquitto_evt_acl_check *e=data;
 if(e->access==MOSQ_ACL_WRITE && TPIsClosed((uintptr_t)e->client)){
  char *topic=NULL;void *payload=NULL;int len=0,retain=0;
  if(TPRewriteWill((uintptr_t)e->client,(char*)e->topic,(void*)e->payload,e->payloadlen,e->qos,e->retain,&topic,&payload,&len,&retain))return MOSQ_ERR_ACL_DENIED;
  int rc=MOSQ_ERR_SUCCESS;
  if(topic)rc=mosquitto_broker_publish_copy(NULL,topic,len,payload,e->qos,retain!=0,NULL);
  free(topic);free(payload);return rc?MOSQ_ERR_ACL_DENIED:MOSQ_ERR_SUCCESS;
 }
 return TPCheckPayload((uintptr_t)e->client,e->access,(char*)e->topic,(void*)e->payload,e->payloadlen,e->qos,e->retain)?MOSQ_ERR_SUCCESS:MOSQ_ERR_ACL_DENIED;
}
static int message_out_cb(int event,void *data,void *unused){
 (void)event;(void)unused;struct mosquitto_evt_message *e=data;
 if(!e->client)return MOSQ_ERR_SUCCESS;
 void *payload=NULL;int len=0;
 int rc=TPOutgoing((uintptr_t)e->client,e->topic,e->payload,e->payloadlen,e->qos,e->retain,&payload,&len);
 if(rc<0)return MOSQ_ERR_ACL_DENIED;
 if(!rc)return MOSQ_ERR_SUCCESS;
 void *b=mosquitto_malloc((size_t)len+1);
 if(!b){free(payload);return MOSQ_ERR_NOMEM;}
 if(len)memcpy(b,payload,(size_t)len);((char*)b)[len]=0;
 free(payload);e->payload=b;e->payloadlen=(uint32_t)len;
 return MOSQ_ERR_SUCCESS;
}
static int message_cb(int event,void *data,void *unused){
 (void)event;(void)unused;struct mosquitto_evt_message *e=data;
 /* Broker-generated observations do not have a device socket. */
 if(!e->client)return MOSQ_ERR_SUCCESS;
 char *topic=NULL;void *payload=NULL;int len=0,retain=0;
 if(TPRewrite((uintptr_t)e->client,e->topic,e->payload,e->payloadlen,e->qos,e->retain,&topic,&payload,&len,&retain))return MOSQ_ERR_ACL_DENIED;
 char *t=mosquitto_strdup(topic);void *b=NULL;
 if(len){b=mosquitto_malloc((size_t)len+1);if(b){memcpy(b,payload,(size_t)len);((char*)b)[len]=0;}}
 free(topic);free(payload);
 if(!t || (len&&!b)){mosquitto_free(t);mosquitto_free(b);return MOSQ_ERR_NOMEM;}
 /* Mosquitto frees the old pointers after a successful callback. */
 e->topic=t;e->payload=b;e->payloadlen=(uint32_t)len;e->retain=retain!=0;
 return MOSQ_ERR_SUCCESS;
}
static int tick_cb(int event,void *data,void *unused){
 (void)event;(void)unused;struct mosquitto_evt_tick *e=data;
 for(int i=0;i<128;i++){int allow=0;char *id=TPPoll(&allow);if(!id)break;mosquitto_complete_basic_auth(id,allow?MOSQ_ERR_SUCCESS:MOSQ_ERR_AUTH);free(id);}
 TPTick();
 for(int i=0;i<128;i++){char *topic=NULL;void *payload=NULL;int len=0;
  if(TPNextObservation(&topic,&payload,&len))break;
  int rc=mosquitto_broker_publish_copy(NULL,topic,len,payload,1,false,NULL);
  if(rc)mosquitto_log_printf(MOSQ_LOG_ERR,"ThingsPanel periodic state observation rejected: %d",rc);
  free(topic);free(payload);
 }
 e->next_ms=20;return MOSQ_ERR_SUCCESS;
}
static void emit_state(struct mosquitto *client,int online){
 char *topic=NULL;void *payload=NULL;int len=0;
 if(!TPObservation((uintptr_t)client,online,&topic,&payload,&len)){
  int rc=mosquitto_broker_publish_copy(NULL,topic,len,payload,1,false,NULL);
  if(rc)mosquitto_log_printf(MOSQ_LOG_ERR,"ThingsPanel state observation rejected by broker: %d",rc);
  free(topic);free(payload);
 }
}
static int connect_cb(int event,void *data,void *unused){(void)event;(void)unused;struct mosquitto_evt_connect *e=data;TPConnected((uintptr_t)e->client);emit_state(e->client,1);return MOSQ_ERR_SUCCESS;}
static int disconnect_cb(int event,void *data,void *unused){(void)event;(void)unused;struct mosquitto_evt_disconnect *e=data;emit_state(e->client,0);TPClosed((uintptr_t)e->client);return MOSQ_ERR_SUCCESS;}
static int offline_cb(int event,void *data,void *unused){(void)event;(void)unused;struct mosquitto_evt_client_offline *e=data;emit_state(e->client,0);TPClosed((uintptr_t)e->client);return MOSQ_ERR_SUCCESS;}
int mosquitto_plugin_init(mosquitto_plugin_id_t *id,void **userdata,struct mosquitto_opt *opts,int count){
 (void)userdata;(void)opts;
 if(count || TPStart())return MOSQ_ERR_INVAL;
 plugin_id=id;mosquitto_plugin_set_info(id,"thingspanel-community","contract-1");
 int events[]={MOSQ_EVT_BASIC_AUTH,MOSQ_EVT_ACL_CHECK,MOSQ_EVT_MESSAGE_IN,MOSQ_EVT_MESSAGE_OUT,MOSQ_EVT_TICK,MOSQ_EVT_CONNECT,MOSQ_EVT_DISCONNECT,MOSQ_EVT_CLIENT_OFFLINE};
 MOSQ_FUNC_generic_callback callbacks[]={auth_cb,acl_cb,message_cb,message_out_cb,tick_cb,connect_cb,disconnect_cb,offline_cb};
 for(unsigned i=0;i<sizeof(events)/sizeof(events[0]);i++){int rc=mosquitto_callback_register(id,events[i],callbacks[i],NULL,NULL);if(rc){TPStop();return rc;}}
 return MOSQ_ERR_SUCCESS;
}
int mosquitto_plugin_cleanup(void *userdata,struct mosquitto_opt *opts,int count){(void)userdata;(void)opts;(void)count;(void)plugin_id;TPStop();return MOSQ_ERR_SUCCESS;}
