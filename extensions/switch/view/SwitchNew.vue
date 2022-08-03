<template>
  <!--插件调试区域01-->
  <div class="chart-out-fE6aI1">
    <div class="chart-all-fE6aI1">
      <!-- <div class="chart-top-fE6aI1">
        <div style="
      text-align:center;
      color: #fff;
      width: 100%;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    "></div>
      </div> -->
      <div class="chart-body-fE6aI1">
        <el-switch
          v-model="status"
          active-color="#13ce66"
          inactive-color="#ff4949"
          @change="switchChange"
        >
        </el-switch>
        <div style="color: #fff">{{ status_title }}</div>
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
      status: true,
      status_title: "已打开",
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
      console.log("latest", latest);

      this.status = false;
      if (latest != undefined && latest["status"] == 1) {
        this.status = true;
      }
      this.status_title = this.status ? "已打开" : "已关闭";
    },

    // change event
    switchChange(val) {
      let _this = this;

      let status = val ? 1 : 0;
      let token = window.localStorage.getItem("id_token");
      console.log("-----token------",window.localStorage.getItem("id_token"))
      // _this.status_title = "操作中";

      //http 推送
      let baseUrl = window.localStorage.getItem("base_url");
      axios({
        method: "POST",
         url: baseUrl + "api/device/operating_device",
        data: {
          device_id: _this.apiData.device_id,
          values: {
            status: status,
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
            _this.status_title = status ? "已打开" : "已关闭";
          }
        })
        .catch(function (error) {
          console.log("error", error);
        });

      //WS发送
      // this.$emit("send", {
      //   status: status,
      // });
    },
  },
  // },
  //async mounted() {},
};
</script>
<!--插件调试区域04-->
<style scoped>
.chart-out-fE6aI1 {
  width: 100%;
  height: 100%;
  position: relative;
}

.chart-all-fE6aI1 {
  width: 100%;
  height: 100%;
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  /* border: 1px solid rgb(41, 189, 139); */
}

/* .chart-top-fE6aI1 {
    padding-left: 0px;
    left: 0px;
    top: 0px;
    width: 100%;
    height: 20px;
    box-sizing: border-box;
   
  } */

.chart-body-fE6aI1 {
  width: 100%;
  height: calc(100% - 50px);
  text-align: center;
  /* border: 2px solid rgb(201, 26, 26); */
}

.chart-out-fE6aI1 .chart-all-fE6aI1 .chart-body-fE6aI1 >>> .el-popover {
  color: #fff !important;
}
</style>