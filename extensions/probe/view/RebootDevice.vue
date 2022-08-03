<template>
  <!--插件调试区域01-->
  <div class="chart-out-nA0aI2">
    <div class="chart-all-nA0aI2">
      <div class="chart-top-nA0aI2">
        <div
          style="
            text-align: center;
            color: #fff;
            width: 100%;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
          "
        >
          <!-- 设备告警列表 -->
        </div>
      </div>
      <div class="chart-body-nA0aI2">
        <el-popconfirm
          confirm-button-text="是"
          cancel-button-text="否"
          @confirm="reboot"
          icon="el-icon-info"
          icon-color="red"
          title="是否确认重启设备？"
        >
          <el-button slot="reference">重启设备</el-button>
        </el-popconfirm>
      </div>
    </div>
  </div>
</template>
<script>
// import { VSwitch } from "vuetify/lib";
import axios from "axios";
export default {
  components: {
    // VSwitch,
    axios,
  },
  name: "DeviceWarning",
  props: {
    loading: {
      type: Boolean,
      default: true,
    },
    apiData: {
      type: Object,
    },
    title: {
      type: String,
      default: "",
    },
  },
  data() {
    return {
      reset: 0,
      // status: true,
      // status_title: "已打开",
      // limit: 10,
      // warningList: [],
    };
  },
  computed: {},
  watch: {
    apiData: {
      immediate: true,
      handler(val, oldVal) {
        console.log("-=-=-=-=-=-=-==-==-=-");
        var _this = this;
        if (!_this.loading) {
          console.log("+++++++++++++++++++");
          _this.initChart();
        }
        // if (!_this.loading) {
        //   setTimeout(() => {
        //     _this.initChart();
        //   }, 1000);

        // }
      },
    },
  },
  methods: {
    /**
     * init chart
     */
    initChart() {
      let fields = this.apiData.fields;
      let latest = fields[fields.length - 1];
      // this.reboot();
      // console.log("apiData11", this.apiData);
      // console.log("latest", latest);
    },
    //http 推送
    reboot() {
      let _this = this;
      let token = window.localStorage.getItem("id_token");
      axios({
        method: "POST",
        url: "http://dev.thingspanel.cn:9999/api/device/reset",
        data: {
          device_id: _this.apiData.device_id,
          values: {
            reset: _this.reset,
          },
        },
        headers: {
          Authorization: "Bearer " + token,
        },
      })
        .then(function (response) {
          let res = response.data;
          console.log("--res----", res);
          if (res.code == 200) {
            _this.$message.success("重启成功！");
            console.log("重启成功！");
          }
        })
        .catch(function (error) {
          console.log("error", error);
        });
    },

    //WS发送
    // this.$emit("send", {
    //   status: status,
    // });
  },
  // },
  async mounted() {},
};
</script>
<!--插件调试区域04-->
<style scoped>
.chart-out-aM3bG9 {
  width: 100%;
  height: 100%;
  position: relative;
}

.chart-all-nA0aI2 {
  width: 100%;
  height: 100%;
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  /* border: 1px solid rgb(41, 189, 139); */
}

.chart-top-nA0aI2 {
  padding-left: 0px;
  left: 0px;
  top: 0px;
  width: 100%;
  height: 50px;
  box-sizing: border-box;
}

.chart-body-nA0aI2 {
  text-align: center;
  width: 100%;
  height: calc(100% - 50px);
  /* border: 2px solid rgb(201, 26, 26); */
}
</style>