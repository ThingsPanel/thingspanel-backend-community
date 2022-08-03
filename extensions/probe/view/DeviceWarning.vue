<template>
  <!--插件调试区域01-->
  <div class="chart-out-aM3bG9">
    <div class="chart-all-kA6iA2">
      <div class="chart-top-kA6iA2">
        <div style="
      text-align:center;
      color: #fff;
      width: 100%;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    ">设备告警列表</div>
      </div>
      <div class="chart-body-kA6iA2">
        <el-table :data="warningList">
          <el-table-column prop="device_name" label="设备名称" width="90">
          </el-table-column>
          <el-table-column prop="describe" label="告警信息" width="300" show-overflow-tooltip>
          </el-table-column>
          <el-table-column prop="created_at" label="时间" :formatter="dateFormat">
          </el-table-column>
        </el-table>
      </div>
    </div>
  </div>
</template>
<script>
  // import { VSwitch } from "vuetify/lib";
  import axios from "axios";
  import moment from 'moment';
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
        // status: true,
        // status_title: "已打开",
        limit: 10,
        warningList: [],
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
      dateFormat: function (row, column) {
        // debugger
        var date = row[column.property];
        if (date == undefined) {
          return "";
        }
        return moment(date * 1000).format("YYYY-MM-DD HH:mm:ss");
      },
      /**
       * init chart
       */
      initChart() {
        let fields = this.apiData.fields;
        let latest = fields[fields.length - 1];
        this.getWarning()
        // console.log("apiData11", this.apiData);
        // console.log("latest", latest);
      },
      //http 推送
      getWarning() {
        let _this = this;
        let token = window.localStorage.getItem("id_token");
        axios({
          method: "POST",
          url: "http://dev.thingspanel.cn:9999/api/warning/view",
          data: {
            device_id: _this.apiData.device_id,
            limit: _this.limit,
          },
          headers: {
            Authorization: "Bearer " + token,
          },
        })
          .then(function (response) {
            let res = response.data;
            console.log("--res----", res);
            if (res.code == 200) {
              _this.warningList = res.data.data;
              console.log("--warningList----", _this.warningList);
            }
          })
          .catch(function (error) {
            console.log("error", error);
          });
      }


      //WS发送
      // this.$emit("send", {
      //   status: status,
      // });
    },
    // },
    async mounted() { },
  };
</script>
<!--插件调试区域04-->
<style scoped>
  .chart-out-aM3bG9 {
    width: 100%;
    height: 100%;
    position: relative;
  }

  .chart-all-kA6iA2 {
    width: 100%;
    height: 100%;
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    /* border: 1px solid rgb(41, 189, 139); */
  }

  .chart-top-kA6iA2 {
    padding-left: 0px;
    left: 0px;
    top: 0px;
    width: 100%;
    height: 20px;
    box-sizing: border-box;
    /* border: 2px solid rgb(24, 222, 50); */
  }

  .chart-body-kA6iA2 {
    width: 100%;
    height: calc(100% - 50px);
    /* border: 2px solid rgb(201, 26, 26); */
  }

  .chart-body-kA6iA2>>>.el-table {
    color: #fff;
  }

  .chart-body-kA6iA2>>>.el-table tr {
    background-color: transparent !important;
  }

  .chart-body-kA6iA2>>>.el-table,
  .el-table::before {
    background-color: transparent !important;
  }

  .chart-body-kA6iA2>>>.el-table thead {
    /* background-color: transparent !important; */
    color: #fff;
  }

  .chart-body-kA6iA2>>>.el-table td.el-table__cell,
  .el-table th.el-table__cell.is-leaf {
    border: none;
    text-align: center;
  }

  .chart-body-kA6iA2>>>.el-table th.el-table__cell {
    background-color: transparent !important;
    border: none;
    text-align: center;
  }

  .chart-body-kA6iA2>>>.el-table--enable-row-hover .el-table__body tr:hover>td.el-table__cell {
    background-color: #f5f5f731;
  }
</style>