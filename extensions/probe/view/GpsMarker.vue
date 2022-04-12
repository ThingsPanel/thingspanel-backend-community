<template>
  <div class="amap-page-container">
    <div class="amap-wrapper" id="markermap">
      <el-amap
        vid="amapGpsMarker"
        :zoom="zoom"
        :center="center"
        :events="mapEvents"
      >
        <el-amap-marker
          v-for="(marker, index) in markers"
          :position="marker.position"
          :title="marker.title"
          :key="index"
        ></el-amap-marker>
      </el-amap>
    </div>
  </div>
</template>
<script>
// import VueAMap from "vue-amap";
export default {
  // components: {
  //   VueAMap,
  // },
  name: "GpsMarker",
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
      markers: [
        {
          title: "测试",
          icon: "img/mark_b.png",
          position: [116.405467, 39.907761],
          text: "测试地址",
        },
      ],
      center: [116.397128, 39.916527],
      zoom: 7,
      // mapStyle: "fresh", //目前支持normal（默认样式）、dark（深色样式）、light（浅色样式）、fresh(osm清新风格样式)四种
      mapEvents: {
        init(o) {
          // o.setMapStyle("amap://styles/c89dd7c67942b35e022a98b0492dd087");
          o.setMapStyle("amap://styles/darkblue");
        },
      },
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
  },
  created() {
    this.initMap();
  },
  methods: {
    initMap() {
      console.log("---------------GPSMARKER1----------------");
      // VueAMap.initAMapApiLoader({
      //   key: "83248e90388dc47d9fb1c4fbd783ea34",
      //   plugin: [
      //     "AMap.Autocomplete", //输入提示插件
      //     "AMap.PlaceSearch", //POI搜索插件
      //     "AMap.Scale", //右下角缩略图插件 比例尺
      //     "AMap.OverView", //地图鹰眼插件
      //     "AMap.ToolBar", //地图工具条
      //     "AMap.MapType", //类别切换控件，实现默认图层与卫星图、实施交通图层之间切换的控制
      //     "AMap.PolyEditor", //编辑 折线多，边形
      //     "AMap.CircleEditor", //圆形编辑器插件
      //     "AMap.Geolocation", //定位控件，用来获取和展示用户主机所在的经纬度位置
      //   ],
      // });
    },

    /**
     * init chart
     */
    initChart() {
      console.log("---------------GPSMARKER2----------------");

      let fields = this.apiData.fields;
      let latest = fields[fields.length - 1];
      // let markers = [];
      for (let i = 0; i < fields.length; i++) {
        this.markers.push({
          title: fields[i].title,
          icon: "img/mark_b.png",
          position: [fields[i].lng, fields[i].lat],
          text: fields[i].address,
        });
      }
      // this.markers = markers;
      this.center = [latest.lng, latest.lat];

      console.log("markers", this.markers);
      console.log("center", this.center);
    },
  },
  async mounted() {},
};
</script>

<style scoped>
.amap-page-container{height: calc(100% - 30px); width: 100%;}
.amap-wrapper {
  width: 100%;
  height: 100%;
}
</style>
