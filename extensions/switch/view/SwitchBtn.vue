<template>
  <div class="v-application v-application--is-ltr">
    <div class="text-center" style="width: 150px; margin: 10px auto 0">
      <div class="bg-primary px-0 py-4 rounded mb-5">
        <h6 style="color: #fff; font-weight: bold">{{ title }}</h6>
        <div class="text-center inlne-block">
          <div style="width: 50px; margin: 0 auto; display: block">
            <v-switch
              v-model="status"
              color="success"
              inset
              @change="switchChange"
            ></v-switch>
          </div>
        </div>
        <div style="color: #fff">{{ status_title }}</div>
      </div>
    </div>
  </div>
</template>
<script>
import { VSwitch } from "vuetify/lib";
import axios from "axios";
export default {
  components: {
    VSwitch,
    axios,
  },
  name: "SwitchBtn",
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
        var _this = this;
        if (!_this.loading) {
          _this.initChart();
        }
      },
    },
    // status: {
    //   immediate: true,
    //   handler(val, oldVal) {
    //     this.status_title = val ? "已打开" : "已关闭";
    //   },
    // },
  },
  methods: {
    /**
     * init chart
     */
    initChart() {
      let fields = this.apiData.fields;
      let latest = fields[fields.length - 1];

      console.log("apiData11", this.apiData);
      console.log("latest", latest);

      this.status = false;
      if (latest != undefined && latest["status"]) {
        this.status = true;
      }

      this.status_title = this.status ? "已打开" : "已关闭";
    },

    // change event
    switchChange(val) {
      let _this = this;

      let status = val ? 1 : 0;
      let token = window.localStorage.getItem("id_token");
      _this.status_title = "操作中";

      //http 推送
      axios({
        method: "POST",
        url: "http://dev.thingspanel.cn:9999/api/device/operating_device",
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
  async mounted() {},
};
</script>
